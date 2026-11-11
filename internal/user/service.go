package user

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PasswordHasher задаёт способ безопасно преобразовать пароль в строку для хранения.
type PasswordHasher interface{ Hash(string) (string, error) }

// Service проверяет правила пользователей и ролей перед обращением к хранилищу.
type Service struct {
	repository Repository
	hasher     PasswordHasher
}

// NewService связывает хранилище пользователей со средством хеширования паролей.
func NewService(repository Repository, hasher PasswordHasher) *Service {
	return &Service{repository: repository, hasher: hasher}
}

// Register создаёт обычного покупателя; роль всегда берётся из системного набора, а не из запроса.
func (s *Service) Register(ctx context.Context, name, email, phone, password string) (User, error) {
	return s.create(ctx, name, email, phone, ClientRoleID, password)
}

// Create проверяет роль и переданные данные, затем создаёт пользователя.
func (s *Service) Create(ctx context.Context, input CreateInput) (User, error) {
	if _, err := uuid.Parse(input.RoleID); err != nil {
		return User{}, fmt.Errorf("%w: invalid role_id", ErrInvalid)
	}
	return s.create(ctx, input.Name, input.Email, input.Phone, input.RoleID, input.Password)
}

// BootstrapAdmin подготавливает первого администратора; повторный запуск отклоняется хранилищем.
func (s *Service) BootstrapAdmin(ctx context.Context, name, email, phone, password string) (User, error) {
	return s.createFirstAdmin(ctx, name, email, phone, password)
}

// create собирает нового пользователя и сохраняет его после проверки входных данных.
func (s *Service) create(ctx context.Context, name, email, phone, roleID, password string) (User, error) {
	item, err := s.newUser(ctx, name, email, phone, roleID, password)
	if err != nil {
		return User{}, err
	}
	if err := s.repository.Create(ctx, item); err != nil {
		return User{}, err
	}
	return item, nil
}

// createFirstAdmin создаёт пользователя с ролью admin через защиту от повторного bootstrap.
func (s *Service) createFirstAdmin(ctx context.Context, name, email, phone, password string) (User, error) {
	item, err := s.newUser(ctx, name, email, phone, AdminRoleID, password)
	if err != nil {
		return User{}, err
	}
	if err := s.repository.CreateFirstAdmin(ctx, item); err != nil {
		return User{}, err
	}
	return item, nil
}

// newUser проверяет профиль и роль, хеширует пароль и готовит запись с новым UUID.
func (s *Service) newUser(ctx context.Context, name, email, phone, roleID, password string) (User, error) {
	normalizedEmail, err := ValidateIdentity(name, email, phone)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	role, err := s.repository.RoleByID(ctx, roleID)
	if err != nil {
		return User{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	item := User{ID: uuid.NewString(), Name: strings.TrimSpace(name), Email: normalizedEmail,
		Phone: strings.TrimSpace(phone), Role: role, RegistrationDate: time.Now().UTC().Format(time.RFC3339Nano), PasswordHash: hash}
	return item, nil
}

// ValidatePassword требует пароль длиной от 12 до 1024 байт; в UTF-8 один символ может занимать несколько байт.
// Нижняя граница отсекает слишком короткие пароли, верхняя ограничивает размер входа.
// Байты используются одинаково для латиницы и многобайтового текста на сервере.
func ValidatePassword(password string) error {
	if len(password) < 12 || len(password) > 1024 {
		return fmt.Errorf("password must contain 12 to 1024 bytes")
	}
	return nil
}

// Get проверяет UUID и загружает пользователя по его идентификатору.
func (s *Service) Get(ctx context.Context, id string) (User, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return User{}, fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.Get(ctx, parsed.String())
}

// Update проверяет UUID, профиль и существование роли перед заменой полей пользователя.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (User, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return User{}, fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	normalizedEmail, err := ValidateIdentity(input.Name, input.Email, input.Phone)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := uuid.Parse(input.RoleID); err != nil {
		return User{}, fmt.Errorf("%w: invalid role_id", ErrInvalid)
	}
	if _, err := s.repository.RoleByID(ctx, input.RoleID); err != nil {
		return User{}, err
	}
	input.Name, input.Email, input.Phone = strings.TrimSpace(input.Name), normalizedEmail, strings.TrimSpace(input.Phone)
	return s.repository.Update(ctx, parsed.String(), input)
}

// Delete проверяет UUID; хранилище не удалит пользователя с заказами или последнего администратора.
func (s *Service) Delete(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.Delete(ctx, parsed.String())
}

// List проверяет параметры страницы и при необходимости фильтрует пользователей по существующей роли.
func (s *Service) List(ctx context.Context, values url.Values, roleID string) (Page, error) {
	limit, offset, err := parsePage(values)
	if err != nil {
		return Page{}, err
	}
	if roleID != "" {
		parsed, err := uuid.Parse(roleID)
		if err != nil {
			return Page{}, fmt.Errorf("%w: invalid role UUID", ErrInvalid)
		}
		roleID = parsed.String()
		if _, err := s.repository.RoleByID(ctx, roleID); err != nil {
			return Page{}, err
		}
	}
	items, total, err := s.repository.List(ctx, limit, offset, roleID)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

// parsePage читает limit и offset, применяет значения по умолчанию и проверяет их границы.
func parsePage(values url.Values) (int, int, error) {
	for key := range values {
		if key != "limit" && key != "offset" {
			return 0, 0, fmt.Errorf("%w: unknown query parameter %q", ErrInvalid, key)
		}
		if len(values[key]) != 1 {
			return 0, 0, fmt.Errorf("%w: repeated query parameter %q", ErrInvalid, key)
		}
	}
	limit, offset := DefaultLimit, 0
	if value := values.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > MaxLimit {
			return 0, 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalid, MaxLimit)
		}
		limit = parsed
	}
	if value := values.Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("%w: offset must be a non-negative integer", ErrInvalid)
		}
		offset = parsed
	}
	return limit, offset, nil
}

// ListRoles возвращает роли, доступные в хранилище.
func (s *Service) ListRoles(ctx context.Context) ([]Role, error) { return s.repository.ListRoles(ctx) }

// CreateRole проверяет название и запрещает занимать имена встроенных ролей.
func (s *Service) CreateRole(ctx context.Context, input RoleInput) (Role, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := ValidateRoleName(input.Name); err != nil {
		return Role{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if IsSystemRoleName(input.Name) {
		return Role{}, fmt.Errorf("%w: system role names are reserved", ErrConflict)
	}
	item := Role{ID: uuid.NewString(), Name: input.Name}
	if err := s.repository.CreateRole(ctx, item); err != nil {
		return Role{}, err
	}
	return item, nil
}

// UpdateRole проверяет идентификатор и новое название, не позволяя переименовать в системную роль.
func (s *Service) UpdateRole(ctx context.Context, id string, input RoleInput) (Role, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Role{}, fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	input.Name = strings.TrimSpace(input.Name)
	if err := ValidateRoleName(input.Name); err != nil {
		return Role{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if IsSystemRoleName(input.Name) {
		return Role{}, fmt.Errorf("%w: system role names are reserved", ErrConflict)
	}
	return s.repository.UpdateRole(ctx, parsed.String(), input)
}

// DeleteRole проверяет идентификатор и удаляет роль, если правила хранилища это разрешают.
func (s *Service) DeleteRole(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.DeleteRole(ctx, parsed.String())
}

// IsNotFound позволяет определить, означает ли ошибка отсутствие пользователя или роли.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
