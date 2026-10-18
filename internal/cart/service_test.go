package cart

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/migrations"
)

const (
	testUserID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherUserID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testProductID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func cartFixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "cart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct{ id, mail string }{{testUserID, "cart1@example.test"}, {otherUserID, "cart2@example.test"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users(id,name,email,phone,role_id,registration_date,password_hash) VALUES(?,?,?,?,?,?,?)`, u.id, "Test", u.mail, "+100000000", "11111111-1111-4111-8111-111111111111", "2026-10-18T00:00:00Z", "hash"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO products(id,name,type,gender,size,color,description,brand,season,price) VALUES(?,?,?,?,?,?,?,?,?,?)`, testProductID, "Shirt", "top", "unisex", "M", "blue", "", "Brand", "summer", 12345); err != nil {
		t.Fatal(err)
	}
	return NewService(NewSQLRepository(db)), ctx
}

func TestCartOperationsAndOwnerIsolation(t *testing.T) {
	s, ctx := cartFixture(t)
	owner := Actor{UserID: testUserID, Role: "client"}
	other := Actor{UserID: otherUserID, Role: "client"}
	item, err := s.Add(ctx, owner, AddInput{ProductID: testProductID, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	if item.Count != 2 || item.Price != 12345 {
		t.Fatalf("added cart item = %+v", item)
	}
	item, err = s.Add(ctx, owner, AddInput{ProductID: testProductID, Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if item.Count != 5 {
		t.Fatalf("increment count=%d, want 5", item.Count)
	}
	items, err := s.List(ctx, owner, testUserID)
	if err != nil || len(items) != 1 {
		t.Fatalf("owner list = %v, %v", items, err)
	}
	if _, err := s.List(ctx, other, testUserID); err != ErrNotFound {
		t.Fatalf("foreign cart list error=%v, want not found", err)
	}
	if _, err := s.Update(ctx, other, item.ID, 4); err != ErrNotFound {
		t.Fatalf("foreign update error=%v, want not found", err)
	}
	updated, err := s.Update(ctx, owner, item.ID, 4)
	if err != nil || updated.Count != 4 {
		t.Fatalf("owner update = %+v, %v", updated, err)
	}
	if err := s.Delete(ctx, owner, item.ID); err != nil {
		t.Fatal(err)
	}
	items, err = s.List(ctx, owner, testUserID)
	if err != nil || len(items) != 0 {
		t.Fatalf("cart after delete=%v, %v", items, err)
	}
}

func TestCartRejectsInvalidQuantityAndNonClient(t *testing.T) {
	s, ctx := cartFixture(t)
	if _, err := s.Add(ctx, Actor{UserID: testUserID, Role: "client"}, AddInput{ProductID: testProductID, Count: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("count 0 error=%v", err)
	}
	if _, err := s.List(ctx, Actor{UserID: testUserID, Role: "manager"}, testUserID); err != ErrDenied {
		t.Fatalf("manager cart access error=%v", err)
	}
}
