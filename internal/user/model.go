package user

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

const (
	ClientRoleID  = "11111111-1111-4111-8111-111111111111"
	ManagerRoleID = "22222222-2222-4222-8222-222222222222"
	AdminRoleID   = "33333333-3333-4333-8333-333333333333"
	DefaultLimit  = 20
	MaxLimit      = 100
)

type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type User struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	Role             Role   `json:"role"`
	RegistrationDate string `json:"registration_date"`
	PasswordHash     string `json:"-"`
}

type CreateInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RoleID   string `json:"role_id"`
	Password string `json:"password"`
}

type UpdateInput struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	RoleID string `json:"role_id"`
}

type RoleInput struct {
	Name string `json:"name"`
}

type Page struct {
	Items  []User `json:"items"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Total  int64  `json:"total"`
}

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

func NormalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
		return "", fmt.Errorf("email must be a valid address")
	}
	return strings.ToLower(value), nil
}

func ValidateIdentity(name, email, phone string) (string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 200 {
		return "", fmt.Errorf("name must contain 1 to 200 characters")
	}
	normalizedEmail, err := NormalizeEmail(email)
	if err != nil || len(normalizedEmail) > 254 {
		return "", fmt.Errorf("email must be a valid address of at most 254 characters")
	}
	if strings.TrimSpace(phone) == "" || len(phone) > 40 {
		return "", fmt.Errorf("phone must contain 1 to 40 characters")
	}
	return normalizedEmail, nil
}

func ValidateRoleName(name string) error {
	if !roleNamePattern.MatchString(name) {
		return fmt.Errorf("role name must be 2 to 32 lowercase letters, digits, '_' or '-' and start with a letter")
	}
	return nil
}

func IsSystemRoleID(id string) bool {
	return id == ClientRoleID || id == ManagerRoleID || id == AdminRoleID
}

func IsSystemRoleName(name string) bool {
	switch strings.ToLower(name) {
	case "client", "manager", "admin":
		return true
	default:
		return false
	}
}
