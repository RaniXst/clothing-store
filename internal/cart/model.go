package cart

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid cart input")
	ErrNotFound = errors.New("cart item not found")
	ErrDenied   = errors.New("cart access denied")
	ErrConflict = errors.New("cart quantity overflow")
)

type Item struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Price     int64  `json:"price"`
	Count     int64  `json:"count"`
}

type AddInput struct {
	ProductID string `json:"product_id"`
	Count     int64  `json:"count"`
}
type Actor struct {
	UserID string
	Role   string
}

func (in AddInput) Validate() (string, error) {
	if in.Count < 1 {
		return "", fmt.Errorf("%w: count must be positive", ErrInvalid)
	}
	id, err := uuid.Parse(strings.TrimSpace(in.ProductID))
	if err != nil {
		return "", fmt.Errorf("%w: product_id must be a UUID", ErrInvalid)
	}
	return id.String(), nil
}

func normalizeID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: id must be a UUID", ErrInvalid)
	}
	return id.String(), nil
}
