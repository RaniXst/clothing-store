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

type PasswordHasher interface{ Hash(string) (string, error) }

type Service struct {
	repository Repository
	hasher     PasswordHasher
}

func NewService(repository Repository, hasher PasswordHasher) *Service {
	return &Service{repository: repository, hasher: hasher}
}

func (s *Service) Register(ctx context.Context, name, email, phone, password string) (User, error) {
	return s.create(ctx, name, email, phone, ClientRoleID, password)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (User, error) {
	if _, err := uuid.Parse(input.RoleID); err != nil {
		return User{}, fmt.Errorf("%w: invalid role_id", ErrInvalid)
	}
	return s.create(ctx, input.Name, input.Email, input.Phone, input.RoleID, input.Password)
}

func (s *Service) BootstrapAdmin(ctx context.Context, name, email, phone, password string) (User, error) {
	return s.createFirstAdmin(ctx, name, email, phone, password)
}

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

func ValidatePassword(password string) error {
	if len(password) < 12 || len(password) > 1024 {
		return fmt.Errorf("password must contain 12 to 1024 bytes")
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id string) (User, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return User{}, fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.Get(ctx, parsed.String())
}

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

func (s *Service) Delete(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.Delete(ctx, parsed.String())
}

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

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) { return s.repository.ListRoles(ctx) }

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

func (s *Service) DeleteRole(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return s.repository.DeleteRole(ctx, parsed.String())
}

func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
