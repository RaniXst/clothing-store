package photo

import (
	"context"
	"database/sql"
	"errors"
)

// SQLRepository хранит метаданные фотографий в SQLite; сами файлы лежат отдельно.
type SQLRepository struct{ db *sql.DB }

// NewSQLRepository создаёт хранилище метаданных фотографий.
func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

// Create записывает UUID, имя файла и UUID товара в таблицу photos.
func (r *SQLRepository) Create(ctx context.Context, item Photo) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO photos (id, filename, product_id) VALUES (?, ?, ?)",
		item.ID, item.Filename, item.ProductID)
	return err
}

// Get загружает метаданные фотографии по UUID.
func (r *SQLRepository) Get(ctx context.Context, id string) (Photo, error) {
	var item Photo
	err := r.db.QueryRowContext(ctx,
		"SELECT id, filename, product_id FROM photos WHERE id = ?", id).
		Scan(&item.ID, &item.Filename, &item.ProductID)
	if errors.Is(err, sql.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	return item, err
}

// Delete удаляет метаданные фотографии и сообщает, если такой записи не было.
func (r *SQLRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM photos WHERE id = ?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ListFilenamesForProduct возвращает имена файлов всех фотографий заданного товара.
func (r *SQLRepository) ListFilenamesForProduct(ctx context.Context, productID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT filename FROM photos WHERE product_id = ? ORDER BY id", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var filenames []string
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		filenames = append(filenames, filename)
	}
	return filenames, rows.Err()
}

// ListFilenames возвращает имена файлов, на которые ссылаются записи SQLite.
func (r *SQLRepository) ListFilenames(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT filename FROM photos ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var filenames []string
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		filenames = append(filenames, filename)
	}
	return filenames, rows.Err()
}
