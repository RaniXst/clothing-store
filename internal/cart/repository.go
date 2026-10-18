package cart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
)

type Repository interface {
	Add(context.Context, string, AddInput) (Item, error)
	List(context.Context, string) ([]Item, error)
	Update(context.Context, string, string, int64) (Item, error)
	Delete(context.Context, string, string) error
}

type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

func (r *SQLRepository) Add(ctx context.Context, userID string, input AddInput) (Item, error) {
	var exists int
	err := r.db.QueryRowContext(ctx, "SELECT 1 FROM products WHERE id=?", input.ProductID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("check cart product: %w", err)
	}
	id := uuid.NewString()
	result, err := r.db.ExecContext(ctx, `INSERT INTO cart(id,user_id,product_id,count) VALUES(?,?,?,?)
		ON CONFLICT(user_id,product_id) DO UPDATE SET count=cart.count+excluded.count WHERE cart.count<=?`, id, userID, input.ProductID, input.Count, int64(math.MaxInt64)-input.Count)
	if err != nil {
		return Item{}, fmt.Errorf("add cart item: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Item{}, fmt.Errorf("check added cart item: %w", err)
	}
	if changed == 0 {
		return Item{}, ErrConflict
	}
	return r.getByProduct(ctx, userID, input.ProductID)
}

func (r *SQLRepository) getByProduct(ctx context.Context, userID, productID string) (Item, error) {
	var item Item
	err := r.db.QueryRowContext(ctx, `SELECT c.id,c.user_id,c.product_id,p.name,p.price,c.count FROM cart c
		JOIN products p ON p.id=c.product_id WHERE c.user_id=? AND c.product_id=?`, userID, productID).
		Scan(&item.ID, &item.UserID, &item.ProductID, &item.Name, &item.Price, &item.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read cart item: %w", err)
	}
	return item, nil
}

func (r *SQLRepository) List(ctx context.Context, userID string) ([]Item, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.user_id,c.product_id,p.name,p.price,c.count FROM cart c
		JOIN products p ON p.id=c.product_id WHERE c.user_id=? ORDER BY c.rowid`, userID)
	if err != nil {
		return nil, fmt.Errorf("list cart: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.UserID, &item.ProductID, &item.Name, &item.Price, &item.Count); err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cart: %w", err)
	}
	return items, nil
}

func (r *SQLRepository) Update(ctx context.Context, userID, cartID string, count int64) (Item, error) {
	result, err := r.db.ExecContext(ctx, "UPDATE cart SET count=? WHERE id=? AND user_id=?", count, cartID, userID)
	if err != nil {
		return Item{}, fmt.Errorf("update cart item: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Item{}, fmt.Errorf("check updated cart item: %w", err)
	}
	if affected == 0 {
		return Item{}, ErrNotFound
	}
	var productID string
	if err := r.db.QueryRowContext(ctx, "SELECT product_id FROM cart WHERE id=?", cartID).Scan(&productID); err != nil {
		return Item{}, fmt.Errorf("read updated cart item: %w", err)
	}
	return r.getByProduct(ctx, userID, productID)
}

func (r *SQLRepository) Delete(ctx context.Context, userID, cartID string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM cart WHERE id=? AND user_id=?", cartID, userID)
	if err != nil {
		return fmt.Errorf("delete cart item: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted cart item: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
