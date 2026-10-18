package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStageDeleteRestoreAndPurge(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := uuid.NewString() + ".png"
	temp, err := store.Prepare([]byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(temp, name); err != nil {
		t.Fatal(err)
	}
	staged, err := store.StageDelete([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(name); err == nil {
		t.Fatal("staged file should not be readable from public upload root")
	}
	if err := store.Restore(staged); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(name); err != nil {
		t.Fatalf("restored file is missing: %v", err)
	}
	staged, err = store.StageDelete([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Purge(staged); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, name)); !os.IsNotExist(err) {
		t.Fatalf("file still exists after purge: %v", err)
	}
}

func TestRecoverRestoresReferencedTombstoneAndRemovesOrphan(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	referenced := uuid.NewString() + ".jpg"
	temp, _ := store.Prepare([]byte("image"))
	if err := store.Publish(temp, referenced); err != nil {
		t.Fatal(err)
	}
	staged, err := store.StageDelete([]string{referenced})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(map[string]struct{}{referenced: {}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(referenced); err != nil {
		t.Fatalf("referenced tombstone was not restored: %v", err)
	}
	if err := store.Purge(staged); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	orphan := uuid.NewString() + ".png"
	temp, _ = store.Prepare([]byte("orphan"))
	if err := store.Publish(temp, orphan); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(store.root, orphan), old, old); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(map[string]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, orphan)); !os.IsNotExist(err) {
		t.Fatalf("orphan file was not removed: %v", err)
	}
}

func TestRecoverDeletesUnreferencedTombstone(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := uuid.NewString() + ".png"
	temp, _ := store.Prepare([]byte("image"))
	if err := store.Publish(temp, name); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StageDelete([]string{name}); err != nil {
		t.Fatal(err)
	}
	if err := store.Recover(map[string]struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.trashDir(), name)); !os.IsNotExist(err) {
		t.Fatalf("unreferenced tombstone remains: %v", err)
	}
}
