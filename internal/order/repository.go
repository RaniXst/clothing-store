package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Checkout(context.Context, string) (Order, error)
	List(context.Context, ListOptions, string) (Page, error)
	Get(context.Context, string) (Order, error)
	UpdateStatus(context.Context, string, string, string) (Order, error)
	AddItem(context.Context, AddItemInput, string) (Item, error)
	Items(context.Context, string) ([]Item, error)
}
type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

func (r *SQLRepository) Checkout(ctx context.Context, userID string) (Order, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin checkout: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT c.product_id,c.count,p.price FROM cart c JOIN products p ON p.id=c.product_id WHERE c.user_id=? ORDER BY c.rowid`, userID)
	if err != nil {
		return Order{}, fmt.Errorf("read checkout cart: %w", err)
	}
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ProductID, &item.Count, &item.Price); err != nil {
			_ = rows.Close()
			return Order{}, fmt.Errorf("read checkout item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Order{}, fmt.Errorf("iterate checkout cart: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Order{}, fmt.Errorf("close checkout cart: %w", err)
	}
	if len(items) == 0 {
		return Order{}, ErrEmptyCart
	}
	var total int64
	for _, line := range items {
		total, err = addAmount(total, line.Price, line.Count)
		if err != nil {
			return Order{}, ErrConflict
		}
	}
	item := Order{ID: uuid.NewString(), UserID: userID, DateTime: time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), Status: StatusAccepted, Items: make([]Item, 0, len(items))}
	item.Total = total
	if _, err := tx.ExecContext(ctx, "INSERT INTO orders(id,user_id,datetime,status) VALUES(?,?,?,?)", item.ID, item.UserID, item.DateTime, item.Status); err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	for _, line := range items {
		line.ID = uuid.NewString()
		line.OrderID = item.ID
		if _, err := tx.ExecContext(ctx, "INSERT INTO order_items(id,order_id,product_id,price,count) VALUES(?,?,?,?,?)", line.ID, line.OrderID, line.ProductID, line.Price, line.Count); err != nil {
			return Order{}, fmt.Errorf("create order item: %w", err)
		}
		item.Items = append(item.Items, line)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM cart WHERE user_id=?", userID); err != nil {
		return Order{}, fmt.Errorf("clear cart: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit checkout: %w", err)
	}
	return item, nil
}

func (r *SQLRepository) List(ctx context.Context, o ListOptions, userID string) (Page, error) {
	where := " WHERE 1=1"
	args := []any{}
	if userID != "" {
		where += " AND user_id=?"
		args = append(args, userID)
	}
	if o.Status != "" {
		where += " AND status=?"
		args = append(args, o.Status)
	}
	if o.From != "" {
		where += " AND datetime>=?"
		args = append(args, o.From)
	}
	if o.ToExclusive != "" {
		where += " AND datetime<?"
		args = append(args, o.ToExclusive)
	}
	var page Page
	page.Limit = o.Limit
	page.Offset = o.Offset
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM orders"+where, args...).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("count orders: %w", err)
	}
	queryArgs := append(append([]any{}, args...), o.Limit, o.Offset)
	rows, err := r.db.QueryContext(ctx, "SELECT id,user_id,datetime,status FROM orders"+where+" ORDER BY datetime DESC,id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return Page{}, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()
	page.Items = make([]Order, 0)
	for rows.Next() {
		var item Order
		if err := rows.Scan(&item.ID, &item.UserID, &item.DateTime, &item.Status); err != nil {
			return Page{}, fmt.Errorf("scan order: %w", err)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate orders: %w", err)
	}
	return page, nil
}

func (r *SQLRepository) Get(ctx context.Context, id string) (Order, error) {
	var item Order
	err := r.db.QueryRowContext(ctx, "SELECT id,user_id,datetime,status FROM orders WHERE id=?", id).Scan(&item.ID, &item.UserID, &item.DateTime, &item.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("read order: %w", err)
	}
	item.Items, err = r.Items(ctx, id)
	if err != nil {
		return Order{}, err
	}
	for _, line := range item.Items {
		item.Total, err = addAmount(item.Total, line.Price, line.Count)
		if err != nil {
			return Order{}, ErrConflict
		}
	}
	return item, nil
}

func (r *SQLRepository) UpdateStatus(ctx context.Context, id, expected, status string) (Order, error) {
	result, err := r.db.ExecContext(ctx, "UPDATE orders SET status=? WHERE id=? AND status=?", status, id, expected)
	if err != nil {
		return Order{}, fmt.Errorf("update order status: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Order{}, fmt.Errorf("check changed order status: %w", err)
	}
	if changed == 0 {
		return Order{}, ErrConflict
	}
	return r.Get(ctx, id)
}

func (r *SQLRepository) AddItem(ctx context.Context, input AddItemInput, productID string) (Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("begin add order item: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM orders WHERE id=?", input.OrderID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	} else if err != nil {
		return Item{}, fmt.Errorf("read target order: %w", err)
	}
	if status != StatusAccepted {
		return Item{}, ErrConflict
	}
	var price int64
	if err := tx.QueryRowContext(ctx, "SELECT price FROM products WHERE id=?", productID).Scan(&price); errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	} else if err != nil {
		return Item{}, fmt.Errorf("read product price: %w", err)
	}
	var currentTotal int64
	rows, err := tx.QueryContext(ctx, "SELECT price,count FROM order_items WHERE order_id=?", input.OrderID)
	if err != nil {
		return Item{}, fmt.Errorf("read existing order item totals: %w", err)
	}
	for rows.Next() {
		var oldPrice, oldCount int64
		if err := rows.Scan(&oldPrice, &oldCount); err != nil {
			_ = rows.Close()
			return Item{}, fmt.Errorf("scan existing order item: %w", err)
		}
		currentTotal, err = addAmount(currentTotal, oldPrice, oldCount)
		if err != nil {
			_ = rows.Close()
			return Item{}, ErrConflict
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return Item{}, fmt.Errorf("iterate existing order items: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Item{}, fmt.Errorf("close existing order items: %w", err)
	}
	if _, err := addAmount(currentTotal, price, input.Count); err != nil {
		return Item{}, ErrConflict
	}
	item := Item{ID: uuid.NewString(), OrderID: input.OrderID, ProductID: productID, Price: price, Count: input.Count}
	if _, err := tx.ExecContext(ctx, "INSERT INTO order_items(id,order_id,product_id,price,count) VALUES(?,?,?,?,?)", item.ID, item.OrderID, item.ProductID, item.Price, item.Count); err != nil {
		return Item{}, fmt.Errorf("add order item: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("commit order item: %w", err)
	}
	return item, nil
}

func addAmount(total, price, count int64) (int64, error) {
	if price < 0 || count < 1 || (price != 0 && count > math.MaxInt64/price) {
		return total, ErrConflict
	}
	amount := price * count
	if total > math.MaxInt64-amount {
		return total, ErrConflict
	}
	return total + amount, nil
}

func (r *SQLRepository) Items(ctx context.Context, orderID string) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,order_id,product_id,price,count FROM order_items WHERE order_id=? ORDER BY rowid", orderID)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ProductID, &item.Price, &item.Count); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}
	return items, nil
}
