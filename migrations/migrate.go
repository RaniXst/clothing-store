package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"
)

// SQL migrations are embedded in the application binary so startup does not
// depend on the process working directory.
//
//go:embed *.sql
var migrationFiles embed.FS

const ledgerTable = "schema_migrations"

// Apply applies every unapplied migration in filename order. The ledger and
// all pending migrations are committed in one transaction, so an error leaves
// no partially applied schema behind.
func Apply(ctx context.Context, db *sql.DB) error {
	return apply(ctx, db, migrationFiles)
}

func apply(ctx context.Context, db *sql.DB, source fs.FS) error {
	names, err := fs.Glob(source, "*.sql")
	if err != nil {
		return fmt.Errorf("list SQL migrations: %w", err)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no SQL migrations found")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY NOT NULL,
		checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	applied := make(map[string]string)
	rows, err := tx.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read migration ledger row: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate migration ledger: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration ledger rows: %w", err)
	}

	known := make(map[string]struct{}, len(names))
	for _, name := range names {
		version := path.Base(name)
		known[version] = struct{}{}
		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		digest := sha256.Sum256(contents)
		checksum := hex.EncodeToString(digest[:])

		if previous, ok := applied[version]; ok {
			if previous != checksum {
				return fmt.Errorf("migration %s checksum changed after it was applied", version)
			}
			continue
		}

		if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("execute migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (?, ?, ?)",
			version, checksum, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}

	for version := range applied {
		if _, ok := known[version]; !ok {
			return fmt.Errorf("database contains unknown migration %s", version)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
