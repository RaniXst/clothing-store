package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RaniXst/clothing-store/internal/user"
)

// SessionLifetime задаёт срок действия сессии — записи, подтверждающей вход пользователя на сервере.
const SessionLifetime = 12 * time.Hour

// Principal содержит данные пользователя и хеш CSRF-токена.
// CSRF-защита проверяет, что запрос на изменение отправлен владельцем входа, а не чужим сайтом.
type Principal struct {
	User     user.User
	CSRFHash string
}

// Credentials содержит адрес почты и пароль, присланные для входа.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Registration содержит поля профиля и пароль для создания аккаунта клиента.
type Registration struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// IssuedSession содержит профиль и исходные токены, которые обработчик устанавливает в cookie.
type IssuedSession struct {
	User      user.User
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

// Service выполняет регистрацию, вход и проверку сессий через хранилища пользователей и сессий.
type Service struct {
	accounts  AccountRepository
	sessions  SessionRepository
	passwords PasswordHasher
	now       func() time.Time
	dummyHash string
}

// NewService собирает службу входа и готовит фиктивный хеш для выравнивания времени
// проверки неизвестной почты.
func NewService(accounts AccountRepository, sessions SessionRepository) (*Service, error) {
	passwords := PasswordHasher{}
	dummyHash, err := passwords.Hash("dummy password used only for timing equalization")
	if err != nil {
		return nil, err
	}
	return &Service{accounts: accounts, sessions: sessions, passwords: passwords, now: time.Now, dummyHash: dummyHash}, nil
}

// Register всегда назначает публично зарегистрированному пользователю роль client.
func (s *Service) Register(ctx context.Context, input Registration) (IssuedSession, error) {
	// Сначала проверяем профиль и вычисляем хеш пароля, затем сохраняем аккаунт и выдаём сессию.
	if err := user.ValidatePassword(input.Password); err != nil {
		return IssuedSession{}, fmt.Errorf("%w: %v", user.ErrInvalid, err)
	}
	email, err := user.ValidateIdentity(input.Name, input.Email, input.Phone)
	if err != nil {
		return IssuedSession{}, fmt.Errorf("%w: %v", user.ErrInvalid, err)
	}
	role, err := s.accounts.RoleByName(ctx, "client")
	if err != nil {
		return IssuedSession{}, err
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return IssuedSession{}, err
	}
	item := user.User{ID: uuid.NewString(), Name: strings.TrimSpace(input.Name), Email: email,
		Phone: strings.TrimSpace(input.Phone), Role: role, RegistrationDate: s.now().UTC().Format(time.RFC3339Nano), PasswordHash: hash}
	if err := s.accounts.Create(ctx, item); err != nil {
		return IssuedSession{}, err
	}
	issued, err := s.issue(ctx, item)
	if err != nil {
		// Если сессию создать не удалось, удаляем уже сохранённый аккаунт.
		if cleanupErr := s.accounts.Delete(ctx, item.ID); cleanupErr != nil {
			return IssuedSession{}, errors.Join(err, fmt.Errorf("remove account after session failure: %w", cleanupErr))
		}
		return IssuedSession{}, err
	}
	return issued, nil
}

// Login проверяет почту и пароль; неверные данные дают одну ошибку, успешный вход создаёт сессию.
func (s *Service) Login(ctx context.Context, credentials Credentials) (IssuedSession, error) {
	if len(credentials.Password) > 1024 {
		return IssuedSession{}, ErrUnauthenticated
	}
	email, emailErr := user.NormalizeEmail(credentials.Email)
	if emailErr != nil {
		_ = s.passwords.Verify(credentials.Password, s.dummyHash)
		return IssuedSession{}, ErrUnauthenticated
	}
	account, err := s.accounts.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			// Проверка фиктивного значения уменьшает различие во времени ответа для неизвестной почты.
			_ = s.passwords.Verify(credentials.Password, s.dummyHash)
			return IssuedSession{}, ErrUnauthenticated
		}
		return IssuedSession{}, err
	}
	if !s.passwords.Verify(credentials.Password, account.PasswordHash) {
		return IssuedSession{}, ErrUnauthenticated
	}
	return s.issue(ctx, account)
}

// issue возвращает исходные токены для установки cookie, сохраняя в базе только их SHA-256-хеши.
func (s *Service) issue(ctx context.Context, account user.User) (IssuedSession, error) {
	if err := s.sessions.DeleteExpired(ctx, s.now()); err != nil {
		return IssuedSession{}, err
	}
	token, err := randomToken()
	if err != nil {
		return IssuedSession{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return IssuedSession{}, err
	}
	now := s.now().UTC()
	item := Session{TokenHash: tokenDigest(token), CSRFHash: tokenDigest(csrf), UserID: account.ID, CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}
	if err := s.sessions.Create(ctx, item); err != nil {
		return IssuedSession{}, err
	}
	return IssuedSession{User: account, Token: token, CSRFToken: csrf, ExpiresAt: item.ExpiresAt}, nil
}

// Authenticate загружает пользователя заново, чтобы авторизация учитывала его текущую роль.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if len(token) < 40 || len(token) > 128 {
		return Principal{}, ErrUnauthenticated
	}
	item, err := s.sessions.Get(ctx, tokenDigest(token))
	if err != nil {
		return Principal{}, err
	}
	if !s.now().Before(item.ExpiresAt) {
		_ = s.sessions.Delete(ctx, item.TokenHash)
		return Principal{}, ErrUnauthenticated
	}
	// Профиль читается заново, поэтому изменение роли сразу действует для новых запросов.
	account, err := s.accounts.Get(ctx, item.UserID)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return Principal{}, ErrUnauthenticated
		}
		return Principal{}, err
	}
	return Principal{User: account, CSRFHash: item.CSRFHash}, nil
}

// Logout удаляет серверную сессию, найденную по хешу переданного cookie-токена.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, tokenDigest(token))
}

// randomToken создаёт криптографически случайный токен для cookie сессии или CSRF-защиты.
func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// tokenDigest возвращает SHA-256-отпечаток токена для хранения и поиска в базе.
func tokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// constantTimeEqual сначала проверяет совпадение длины строк.
// Если длины одинаковые, сравнивает их содержимое за постоянное время,
// чтобы уменьшить риск утечки информации через время сравнения.
func constantTimeEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
