package database

import (
	"context"
	"database/sql"
	"testing"
)

func TestOpenEnablesForeignKeysOnEveryConnection(t *testing.T) {
	db, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(3)
	connections := make([]*sql.Conn, 0, 3)
	for i := 0; i < 3; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("open connection %d: %v", i+1, err)
		}
		connections = append(connections, conn)
	}
	defer func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()

	for i, connection := range connections {
		var enabled int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			t.Fatalf("query foreign_keys on connection %d: %v", i+1, err)
		}
		if enabled != 1 {
			t.Errorf("foreign_keys on connection %d = %d, want 1", i+1, enabled)
		}
	}
}
