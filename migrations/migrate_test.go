package migrations

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestApplyIsRepeatableAndRecordsMigrations(t *testing.T) {
	db := openMemoryDB(t)
	ctx := context.Background()

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() first run: %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() second run: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if count != 2 {
		t.Fatalf("migration records = %d, want 2", count)
	}
}

func TestApplyRollsBackEverythingOnFailure(t *testing.T) {
	db := openMemoryDB(t)
	badMigrations := fstest.MapFS{
		"001_create.sql": &fstest.MapFile{Data: []byte("CREATE TABLE should_rollback (id INTEGER PRIMARY KEY);")},
		"002_fail.sql":   &fstest.MapFile{Data: []byte("INSERT INTO missing_table VALUES (1);")},
	}

	if err := apply(context.Background(), db, badMigrations); err == nil {
		t.Fatal("apply() expected an error for invalid SQL")
	}

	for _, table := range []string{"should_rollback", ledgerTable} {
		var count int
		err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
		if err != nil {
			t.Fatalf("check table %q: %v", table, err)
		}
		if count != 0 {
			t.Errorf("table %q exists after failed migration; want transaction rollback", table)
		}
	}
}

func TestApplyRejectsChangedAppliedMigration(t *testing.T) {
	db := openMemoryDB(t)
	first := fstest.MapFS{
		"001_schema.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example (id INTEGER PRIMARY KEY);")},
	}
	if err := apply(context.Background(), db, first); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}
	changed := fstest.MapFS{
		"001_schema.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example (id INTEGER PRIMARY KEY, name TEXT);")},
	}
	if err := apply(context.Background(), db, changed); err == nil {
		t.Fatal("apply() expected an error when an applied migration checksum changes")
	}

	var columns int
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('example')").Scan(&columns); err != nil {
		t.Fatalf("inspect example table: %v", err)
	}
	if columns != 1 {
		t.Fatalf("example has %d columns after checksum error, want 1", columns)
	}

}
