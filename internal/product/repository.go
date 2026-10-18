package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

const productColumns = `id, name, type, gender, size, color, description, brand, season, price`

func (r *SQLRepository) Create(ctx context.Context, item Product) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO products (`+productColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Name, item.Type, item.Gender, item.Size, item.Color,
		item.Description, item.Brand, item.Season, item.Price)
	return err
}

func (r *SQLRepository) Get(ctx context.Context, id string) (Product, error) {
	item, err := scanProduct(r.db.QueryRowContext(ctx, `SELECT `+productColumns+` FROM products WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRepository) Update(ctx context.Context, id string, input ProductInput) (Product, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE products SET
		name = ?, type = ?, gender = ?, size = ?, color = ?, description = ?, brand = ?, season = ?, price = ?
		WHERE id = ?`, input.Name, input.Type, input.Gender, input.Size, input.Color,
		input.Description, input.Brand, input.Season, input.Price, id)
	if err != nil {
		return Product{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Product{}, err
	}
	if count == 0 {
		return Product{}, ErrNotFound
	}
	return r.Get(ctx, id)
}

func (r *SQLRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM products WHERE id = ?", id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var orderReferences int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM order_items WHERE product_id = ?", id).Scan(&orderReferences); err != nil {
		return err
	}
	if orderReferences > 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM products WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete product row: %w", err)
	}
	return tx.Commit()
}

func (r *SQLRepository) List(ctx context.Context, options ListOptions) (ProductPage, error) {
	filters := []struct{ column, value string }{
		{"type", options.Type}, {"gender", options.Gender}, {"size", options.Size},
		{"color", options.Color}, {"brand", options.Brand}, {"season", options.Season},
	}
	conditions := make([]string, 0, len(filters))
	args := make([]any, 0, len(filters))
	for _, filter := range filters {
		if filter.value != "" {
			conditions = append(conditions, filter.column+" = ?")
			args = append(args, filter.value)
		}
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM products"+where, args...).Scan(&total); err != nil {
		return ProductPage{}, err
	}

	order := "id ASC"
	switch options.Sort {
	case "price_asc":
		order = "price ASC, id ASC"
	case "price_desc":
		order = "price DESC, id ASC"
	}
	query := "SELECT " + productColumns + " FROM products" + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	queryArgs := append(append([]any(nil), args...), options.Limit, options.Offset)
	rows, err := r.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return ProductPage{}, err
	}
	defer rows.Close()

	items := make([]Product, 0)
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return ProductPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProductPage{}, err
	}
	return ProductPage{Items: items, Limit: options.Limit, Offset: options.Offset, Total: total}, nil
}

type productScanner interface{ Scan(...any) error }

func scanProduct(row productScanner) (Product, error) {
	var item Product
	err := row.Scan(&item.ID, &item.Name, &item.Type, &item.Gender, &item.Size,
		&item.Color, &item.Description, &item.Brand, &item.Season, &item.Price)
	return item, err
}
