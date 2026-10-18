package order

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/migrations"
	"github.com/google/uuid"
)

const (
	clientID      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherClientID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	managerID     = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	adminID       = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	productID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	product2ID    = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

type orderFixture struct {
	ctx                           context.Context
	db                            *sql.DB
	svc                           *Service
	client, other, manager, admin Actor
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	users := []struct{ id, role, name string }{{clientID, "11111111-1111-4111-8111-111111111111", "client1@example.test"}, {otherClientID, "11111111-1111-4111-8111-111111111111", "client2@example.test"}, {managerID, "22222222-2222-4222-8222-222222222222", "manager@example.test"}, {adminID, "33333333-3333-4333-8333-333333333333", "admin@example.test"}}
	for _, u := range users {
		if _, err := db.ExecContext(ctx, `INSERT INTO users(id,name,email,phone,role_id,registration_date,password_hash) VALUES(?,?,?,?,?,?,?)`, u.id, "Test", u.name, "+100000000", u.role, "2026-10-18T00:00:00Z", "hash"); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		id, name string
		price    int64
	}{{productID, "Shirt", 129900}, {product2ID, "Hat", 25000}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO products(id,name,type,gender,size,color,description,brand,season,price) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.id, p.name, "top", "unisex", "M", "blue", "", "Brand", "summer", p.price); err != nil {
			t.Fatal(err)
		}
	}
	return &orderFixture{ctx: ctx, db: db, svc: NewService(NewSQLRepository(db)), client: Actor{UserID: clientID, Role: "client"}, other: Actor{UserID: otherClientID, Role: "client"}, manager: Actor{UserID: managerID, Role: "manager"}, admin: Actor{UserID: adminID, Role: "admin"}}
}

func (f *orderFixture) addCart(product string, count int64) {
	f.tExec(`INSERT INTO cart(id,user_id,product_id,count) VALUES(?,?,?,?)`, uuid.NewString(), clientID, product, count)
}
func (f *orderFixture) tExec(query string, args ...any) {
	_, err := f.db.ExecContext(f.ctx, query, args...)
	if err != nil {
		panic(err)
	}
}

func TestCheckoutSnapshotsPricesClearsCartAndPreventsDuplicate(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 2)
	f.addCart(product2ID, 1)
	created, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusAccepted || len(created.Items) != 2 || created.Total != 284800 {
		t.Fatalf("checkout result=%+v", created)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE products SET price=999999 WHERE id=?", productID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.svc.Get(f.ctx, f.client, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Items[0].Price != 129900 {
		t.Fatalf("stored price=%d", stored.Items[0].Price)
	}
	var cartCount, orderCount int
	if err := f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM cart WHERE user_id=?", clientID).Scan(&cartCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM orders WHERE user_id=?", clientID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if cartCount != 0 || orderCount != 1 {
		t.Fatalf("cart=%d orders=%d", cartCount, orderCount)
	}
	if _, err := f.svc.Checkout(f.ctx, f.client); !errors.Is(err, ErrEmptyCart) {
		t.Fatalf("repeat checkout error=%v", err)
	}
	if err := f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM orders WHERE user_id=?", clientID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("repeat checkout created %d orders", orderCount)
	}
}

func TestCheckoutRollsBackOrderItemsAndCartOnFailure(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 1)
	if _, err := f.db.ExecContext(f.ctx, `CREATE TRIGGER fail_order_item BEFORE INSERT ON order_items BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Checkout(f.ctx, f.client); err == nil {
		t.Fatal("checkout unexpectedly succeeded")
	}
	var orders, items, cart int
	for q, dst := range map[string]*int{"SELECT count(*) FROM orders": &orders, "SELECT count(*) FROM order_items": &items, "SELECT count(*) FROM cart": &cart} {
		if err := f.db.QueryRowContext(f.ctx, q).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if orders != 0 || items != 0 || cart != 1 {
		t.Fatalf("after rollback orders=%d items=%d cart=%d", orders, items, cart)
	}
}

func TestOrderAccessRolesAndStatusTransitions(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 1)
	created, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(f.ctx, f.other, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign get error=%v", err)
	}
	page, err := f.svc.List(f.ctx, f.other, ListOptions{Limit: 20})
	if err != nil || page.Total != 0 {
		t.Fatalf("foreign list=%+v err=%v", page, err)
	}
	if _, err := f.svc.UpdateStatus(f.ctx, f.other, created.ID, StatusCancelled); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign cancel error=%v", err)
	}
	cancelled, err := f.svc.UpdateStatus(f.ctx, f.client, created.ID, StatusCancelled)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("client cancel=%+v %v", cancelled, err)
	}
	if _, err := f.svc.UpdateStatus(f.ctx, f.client, created.ID, StatusAccepted); !errors.Is(err, ErrConflict) {
		t.Fatalf("client changed status error=%v", err)
	}
	f.addCart(productID, 1)
	second, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateStatus(f.ctx, f.manager, second.ID, StatusReady); !errors.Is(err, ErrConflict) {
		t.Fatalf("skipped status error=%v", err)
	}
	for _, status := range []string{StatusWorking, StatusAssembled, StatusReady} {
		second, err = f.svc.UpdateStatus(f.ctx, f.manager, second.ID, status)
		if err != nil || second.Status != status {
			t.Fatalf("transition to %q=%+v err=%v", status, second, err)
		}
	}
	if _, err := f.svc.UpdateStatus(f.ctx, f.admin, second.ID, StatusCancelled); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal cancellation error=%v", err)
	}
	if _, err := f.svc.Items(f.ctx, f.other, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign item list error=%v", err)
	}
	if lines, err := f.svc.Items(f.ctx, f.manager, second.ID); err != nil || len(lines) != 1 {
		t.Fatalf("manager items=%v err=%v", lines, err)
	}
}

func TestManagerAddsOrderItemAtCurrentPrice(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 1)
	created, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	input := AddItemInput{OrderID: created.ID, ProductID: product2ID, Count: 3}
	if _, err := f.svc.AddItem(f.ctx, f.client, input); !errors.Is(err, ErrDenied) {
		t.Fatalf("client add order item error=%v", err)
	}
	line, err := f.svc.AddItem(f.ctx, f.manager, input)
	if err != nil {
		t.Fatal(err)
	}
	if line.Price != 25000 || line.Count != 3 {
		t.Fatalf("added item=%+v", line)
	}
	created, err = f.svc.Get(f.ctx, f.admin, created.ID)
	if err != nil || created.Total != 204900 {
		t.Fatalf("updated order=%+v err=%v", created, err)
	}
	if _, err := f.svc.AddItem(f.ctx, f.admin, AddItemInput{OrderID: created.ID, ProductID: productID, Count: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero quantity error=%v", err)
	}
}

func TestOrderListFiltersAndPaginationValidation(t *testing.T) {
	f := newOrderFixture(t)
	for _, row := range []struct{ id, day, status, uid string }{{"11111111-1111-4111-8111-111111111111", "2026-01-01", StatusAccepted, clientID}, {"22222222-2222-4222-8222-222222222222", "2026-01-02", StatusWorking, clientID}, {"44444444-4444-4444-8444-444444444444", "2026-01-31", StatusAccepted, otherClientID}, {"33333333-3333-4333-8333-333333333333", "2026-02-01", StatusAccepted, otherClientID}} {
		if _, err := f.db.ExecContext(f.ctx, "INSERT INTO orders(id,user_id,datetime,status) VALUES(?,?,?,?)", row.id, row.uid, row.day+"T12:00:00.000000000Z", row.status); err != nil {
			t.Fatal(err)
		}
	}
	opts, err := ParseListOptions(url.Values{"status": {StatusAccepted}, "date_from": {"2026-01-01"}, "date_to": {"2026-01-31"}, "limit": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.svc.List(f.ctx, f.manager, opts)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Limit != 1 {
		t.Fatalf("filtered page=%+v err=%v", page, err)
	}
	own, err := f.svc.List(f.ctx, f.client, opts)
	if err != nil || own.Total != 1 {
		t.Fatalf("client scoped filter=%+v err=%v", own, err)
	}
	for _, query := range []url.Values{{"status": {"bad"}}, {"date_from": {"2026-02-30"}}, {"date_from": {"2026-02-01"}, "date_to": {"2026-01-01"}}, {"limit": {"101"}}, {"offset": {"-1"}}, {"unexpected": {"1"}}} {
		if _, err := ParseListOptions(query); !errors.Is(err, ErrInvalid) {
			t.Errorf("query %v error=%v", query, err)
		}
	}
}

func TestEmptyCartReturnsConflict(t *testing.T) {
	f := newOrderFixture(t)
	if _, err := f.svc.Checkout(f.ctx, f.client); !errors.Is(err, ErrEmptyCart) {
		t.Fatalf("error=%v", err)
	}
}

func TestCheckoutRejectsUnrepresentableTotalWithoutMutatingCart(t *testing.T) {
	f := newOrderFixture(t)
	if _, err := f.db.ExecContext(f.ctx, "UPDATE products SET price=? WHERE id=?", int64(math.MaxInt64), productID); err != nil {
		t.Fatal(err)
	}
	f.addCart(productID, 2)
	if _, err := f.svc.Checkout(f.ctx, f.client); !errors.Is(err, ErrConflict) {
		t.Fatalf("overflow checkout error=%v", err)
	}
	var orderCount, cartCount int
	if err := f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM orders").Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRowContext(f.ctx, "SELECT count(*) FROM cart").Scan(&cartCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 || cartCount != 1 {
		t.Fatalf("after overflow: orders=%d cart=%d", orderCount, cartCount)
	}
}
