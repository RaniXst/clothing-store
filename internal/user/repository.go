package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound   = errors.New("user or role not found")
	ErrConflict   = errors.New("user or role conflict")
	ErrInvalid    = errors.New("invalid user or role")
	ErrSystemRole = errors.New("system roles cannot be renamed or deleted")
	ErrLastAdmin  = errors.New("cannot remove the last administrator")
	ErrHasOrders  = errors.New("user has order history")
)

type Repository interface {
	Create(context.Context, User) error
	CreateFirstAdmin(context.Context, User) error
	Get(context.Context, string) (User, error)
	FindByEmail(context.Context, string) (User, error)
	RoleByName(context.Context, string) (Role, error)
	RoleByID(context.Context, string) (Role, error)
	List(context.Context, int, int, string) ([]User, int64, error)
	Update(context.Context, string, UpdateInput) (User, error)
	Delete(context.Context, string) error
	ListRoles(context.Context) ([]Role, error)
	CreateRole(context.Context, Role) error
	UpdateRole(context.Context, string, RoleInput) (Role, error)
	DeleteRole(context.Context, string) error
	CountAdmins(context.Context) (int, error)
}

type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

const userSelect = `SELECT u.id, u.name, u.email, u.phone, u.role_id, r.name,
	u.registration_date, u.password_hash FROM users u JOIN roles r ON r.id = u.role_id`

func (r *SQLRepository) Create(ctx context.Context, item User) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO users
		(id, name, email, phone, role_id, registration_date, password_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Name, item.Email, item.Phone,
		item.Role.ID, item.RegistrationDate, item.PasswordHash)
	return mapConflict(err)
}

func (r *SQLRepository) CreateFirstAdmin(ctx context.Context, item User) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The no-op update obtains SQLite's write lock before the existence check,
	// making bootstrap safe against two concurrent CLI invocations.
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET name=name WHERE id=?`, AdminRoleID); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role_id=?`, AdminRoleID).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: an administrator already exists", ErrConflict)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users
		(id, name, email, phone, role_id, registration_date, password_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Name, item.Email, item.Phone,
		item.Role.ID, item.RegistrationDate, item.PasswordHash); err != nil {
		return mapConflict(err)
	}
	return tx.Commit()
}

func (r *SQLRepository) Get(ctx context.Context, id string) (User, error) {
	item, err := scanUser(r.db.QueryRowContext(ctx, userSelect+` WHERE u.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRepository) FindByEmail(ctx context.Context, email string) (User, error) {
	item, err := scanUser(r.db.QueryRowContext(ctx, userSelect+` WHERE u.email = ?`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRepository) RoleByName(ctx context.Context, name string) (Role, error) {
	var item Role
	err := r.db.QueryRowContext(ctx, `SELECT id, name FROM roles WHERE name = ? COLLATE NOCASE`, name).Scan(&item.ID, &item.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRepository) RoleByID(ctx context.Context, id string) (Role, error) {
	var item Role
	err := r.db.QueryRowContext(ctx, `SELECT id, name FROM roles WHERE id = ?`, id).Scan(&item.ID, &item.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRepository) List(ctx context.Context, limit, offset int, roleID string) ([]User, int64, error) {
	where, args := "", []any{}
	if roleID != "" {
		where, args = " WHERE u.role_id = ?", append(args, roleID)
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM users u`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := userSelect + where + ` ORDER BY u.registration_date DESC, u.id ASC LIMIT ? OFFSET ?`
	queryArgs := append(append([]any(nil), args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]User, 0)
	for rows.Next() {
		item, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLRepository) Update(ctx context.Context, id string, input UpdateInput) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentRoleID string
	if err := tx.QueryRowContext(ctx, `SELECT role_id FROM users WHERE id = ?`, id).Scan(&currentRoleID); errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	} else if err != nil {
		return User{}, err
	}
	if currentRoleID == AdminRoleID && input.RoleID != AdminRoleID {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role_id = ?`, AdminRoleID).Scan(&count); err != nil {
			return User{}, err
		}
		if count <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE users SET name=?, email=?, phone=?, role_id=? WHERE id=?`, input.Name, input.Email, input.Phone, input.RoleID, id)
	if err != nil {
		return User{}, mapConflict(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if count == 0 {
		return User{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return r.Get(ctx, id)
}

func (r *SQLRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT role_id FROM users WHERE id=?`, id).Scan(&roleID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var orderCount int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE user_id=?`, id).Scan(&orderCount); err != nil {
		return err
	}
	if orderCount > 0 {
		return ErrHasOrders
	}
	if roleID == AdminRoleID {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role_id=?`, AdminRoleID).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Role, 0)
	for rows.Next() {
		var item Role
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SQLRepository) CreateRole(ctx context.Context, item Role) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO roles (id, name) VALUES (?, ?)`, item.ID, item.Name)
	return mapConflict(err)
}

func (r *SQLRepository) UpdateRole(ctx context.Context, id string, input RoleInput) (Role, error) {
	if IsSystemRoleID(id) {
		return Role{}, ErrSystemRole
	}
	result, err := r.db.ExecContext(ctx, `UPDATE roles SET name=? WHERE id=?`, input.Name, id)
	if err != nil {
		return Role{}, mapConflict(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Role{}, err
	}
	if count == 0 {
		return Role{}, ErrNotFound
	}
	return r.RoleByID(ctx, id)
}

func (r *SQLRepository) DeleteRole(ctx context.Context, id string) error {
	if IsSystemRoleID(id) {
		return ErrSystemRole
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM roles WHERE id=?`, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key constraint failed") {
			return ErrConflict
		}
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

func (r *SQLRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role_id=?`, AdminRoleID).Scan(&count)
	return count, err
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var item User
	err := row.Scan(&item.ID, &item.Name, &item.Email, &item.Phone, &item.Role.ID, &item.Role.Name, &item.RegistrationDate, &item.PasswordHash)
	return item, err
}

func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint failed") || strings.Contains(message, "foreign key constraint failed") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}
