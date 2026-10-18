package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/RaniXst/clothing-store/internal/database"
)

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "clothing.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := Apply(context.Background(), db); err != nil {
		_ = db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSchemaAndSeedRoles(t *testing.T) {
	db := openMigratedDB(t)
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	for _, name := range []string{"products", "photos", "roles", "users", "cart", "orders", "order_items", "auth_sessions", ledgerTable} {
		if !tables[name] {
			t.Errorf("table %q is missing", name)
		}
	}
	if len(tables) != 9 {
		t.Errorf("found %d application and migration tables, want 9", len(tables))
	}

	roleRows, err := db.Query("SELECT name FROM roles ORDER BY name")
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	defer roleRows.Close()
	var roles []string
	for roleRows.Next() {
		var role string
		if err := roleRows.Scan(&role); err != nil {
			t.Fatalf("scan role: %v", err)
		}
		roles = append(roles, role)
	}
	if err := roleRows.Err(); err != nil {
		t.Fatalf("iterate roles: %v", err)
	}
	wantRoles := []string{"admin", "client", "manager"}
	if len(roles) != len(wantRoles) {
		t.Fatalf("roles = %v, want %v", roles, wantRoles)
	}
	for i := range wantRoles {
		if roles[i] != wantRoles[i] {
			t.Errorf("roles = %v, want %v", roles, wantRoles)
			break
		}
	}
}

func TestSchemaConstraintsAndForeignKeys(t *testing.T) {
	db := openMigratedDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO products
		(id, name, type, gender, size, color, description, brand, season, price)
		VALUES ('p1', 'T-shirt', 'top', 'unisex', 'M', 'black', 'desc', 'Brand', 'summer', 199900)`); err != nil {
		t.Fatalf("insert valid product: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users
		(id, name, email, phone, role_id, registration_date, password_hash)
		VALUES ('u1', 'Test User', 'test@example.com', '+100000000',
		'11111111-1111-4111-8111-111111111111', '2026-10-16T10:00:00Z', 'hash')`); err != nil {
		t.Fatalf("insert valid user: %v", err)
	}

	assertRejected := func(label, query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err == nil {
			t.Errorf("%s: statement unexpectedly succeeded", label)
		}
	}
	assertRejected("negative product price", `INSERT INTO products
		(id, name, type, gender, size, color, description, brand, season, price)
		VALUES ('p2', 'Jacket', 'outer', 'unisex', 'L', 'blue', 'desc', 'Brand', 'winter', -1)`)
	assertRejected("foreign key", "INSERT INTO photos (id, filename, product_id) VALUES ('ph1', 'p1.jpg', 'missing')")
	assertRejected("zero cart count", "INSERT INTO cart (id, user_id, product_id, count) VALUES ('c1', 'u1', 'p1', 0)")
	assertRejected("invalid order status", "INSERT INTO orders (id, user_id, datetime, status) VALUES ('o1', 'u1', '2026-10-16T10:00:00Z', 'Unknown')")

	var foreignKeyViolations int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	if foreignKeyViolations != 0 {
		t.Errorf("foreign key violations = %d, want 0", foreignKeyViolations)
	}
}
