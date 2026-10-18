package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RaniXst/clothing-store/internal/user"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Session struct {
	TokenHash string
	CSRFHash  string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type SessionRepository interface {
	Create(context.Context, Session) error
	Get(context.Context, string) (Session, error)
	Delete(context.Context, string) error
	DeleteExpired(context.Context, time.Time) error
}

type SQLSessionRepository struct{ db *sql.DB }

func NewSQLSessionRepository(db *sql.DB) *SQLSessionRepository {
	return &SQLSessionRepository{db: db}
}

func (r *SQLSessionRepository) Create(ctx context.Context, item Session) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_sessions
		(token_hash, csrf_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		item.TokenHash, item.CSRFHash, item.UserID,
		formatSessionTime(item.CreatedAt), formatSessionTime(item.ExpiresAt))
	return err
}

func (r *SQLSessionRepository) Get(ctx context.Context, tokenHash string) (Session, error) {
	var item Session
	var createdAt, expiresAt string
	err := r.db.QueryRowContext(ctx, `SELECT token_hash, csrf_hash, user_id, created_at, expires_at
		FROM auth_sessions WHERE token_hash=?`, tokenHash).Scan(&item.TokenHash, &item.CSRFHash, &item.UserID, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse session creation time: %w", err)
	}
	item.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse session expiry: %w", err)
	}
	return item, nil
}

func (r *SQLSessionRepository) Delete(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (r *SQLSessionRepository) DeleteExpired(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, formatSessionTime(now))
	return err
}

func formatSessionTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

type AccountRepository interface {
	Create(context.Context, user.User) error
	Get(context.Context, string) (user.User, error)
	FindByEmail(context.Context, string) (user.User, error)
	RoleByName(context.Context, string) (user.Role, error)
	Delete(context.Context, string) error
}
