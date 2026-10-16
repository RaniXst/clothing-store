package database

import (
	"context"
	"testing"
)

func TestOpenEnablesForeignKeys(t *testing.T) {
	db, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	var enabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil {
		t.Fatalf("failed to query foreign_keys: %v", err)
	}

	if enabled != 1 {
		t.Fatalf("foreign_keys = %d, want 1", enabled)
	}
}
