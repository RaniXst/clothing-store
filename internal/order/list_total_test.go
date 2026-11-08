package order

import (
	"errors"
	"math"
	"testing"
)

func TestListTotalUsesSnapshotPricesAndCounts(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 2)
	f.addCart(product2ID, 3)
	created, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	f.tExec("UPDATE products SET price=1 WHERE id=?", productID)
	for _, actor := range []Actor{f.client, f.manager, f.admin} {
		page, err := f.svc.List(f.ctx, actor, ListOptions{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Total != 1 {
			t.Fatalf("unexpected page: %+v", page)
		}
		if page.Items[0].Total != 334800 || page.Items[0].Total != created.Total {
			t.Fatalf("list total=%d, checkout total=%d; want 334800", page.Items[0].Total, created.Total)
		}
		if len(page.Items[0].Items) != 0 {
			t.Fatal("list should remain a summary")
		}
	}
	page, err := f.svc.List(f.ctx, f.other, ListOptions{Limit: 20})
	if err != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("other client page=%+v, err=%v", page, err)
	}
}

func TestListTotalRejectsOverflow(t *testing.T) {
	f := newOrderFixture(t)
	f.addCart(productID, 1)
	created, err := f.svc.Checkout(f.ctx, f.client)
	if err != nil {
		t.Fatal(err)
	}
	f.tExec("UPDATE order_items SET price=?,count=2 WHERE order_id=?", int64(math.MaxInt64), created.ID)
	_, err = f.svc.List(f.ctx, f.client, ListOptions{Limit: 20})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v, want ErrConflict", err)
	}
}
