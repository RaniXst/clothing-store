package order

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusAccepted  = "Принят"
	StatusWorking   = "В работе"
	StatusAssembled = "Собран"
	StatusReady     = "Готов к выдаче"
	StatusCancelled = "Отменён"
	DefaultLimit    = 20
	MaxLimit        = 100
)

var (
	Statuses     = []string{StatusAccepted, StatusWorking, StatusAssembled, StatusReady, StatusCancelled}
	ErrInvalid   = errors.New("invalid order input")
	ErrNotFound  = errors.New("order not found")
	ErrDenied    = errors.New("order access denied")
	ErrConflict  = errors.New("order state conflict")
	ErrEmptyCart = errors.New("cart is empty")
)

type Actor struct {
	UserID string
	Role   string
}
type Item struct {
	ID        string `json:"id"`
	OrderID   string `json:"order_id"`
	ProductID string `json:"product_id"`
	Price     int64  `json:"price"`
	Count     int64  `json:"count"`
}
type Order struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	DateTime string `json:"datetime"`
	Status   string `json:"status"`
	Items    []Item `json:"items,omitempty"`
	Total    int64  `json:"total"`
}
type Page struct {
	Items  []Order `json:"items"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Total  int64   `json:"total"`
}
type StatusInput struct {
	Status string `json:"status"`
}
type AddItemInput struct {
	OrderID   string `json:"order_id"`
	ProductID string `json:"product_id"`
	Count     int64  `json:"count"`
}
type ListOptions struct {
	Limit, Offset            int
	Status, DateFrom, DateTo string
	From, ToExclusive        string
}

func IsStatus(value string) bool {
	for _, v := range Statuses {
		if value == v {
			return true
		}
	}
	return false
}
func (in AddItemInput) Validate() (string, string, error) {
	if in.Count < 1 {
		return "", "", fmt.Errorf("%w: count must be positive", ErrInvalid)
	}
	o, err := uuid.Parse(strings.TrimSpace(in.OrderID))
	if err != nil {
		return "", "", fmt.Errorf("%w: order_id must be a UUID", ErrInvalid)
	}
	p, err := uuid.Parse(strings.TrimSpace(in.ProductID))
	if err != nil {
		return "", "", fmt.Errorf("%w: product_id must be a UUID", ErrInvalid)
	}
	return o.String(), p.String(), nil
}
func normalizeID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: id must be a UUID", ErrInvalid)
	}
	return id.String(), nil
}

func ParseListOptions(values url.Values) (ListOptions, error) {
	o := ListOptions{Limit: DefaultLimit}
	allowed := map[string]bool{"limit": true, "offset": true, "status": true, "date_from": true, "date_to": true}
	for k, v := range values {
		if !allowed[k] || len(v) != 1 {
			return ListOptions{}, fmt.Errorf("%w: invalid or repeated query parameter %q", ErrInvalid, k)
		}
	}
	parse := func(k string, def int) (int, error) {
		v, ok := values[k]
		if !ok {
			return def, nil
		}
		n, e := strconv.Atoi(v[0])
		if e != nil {
			return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalid, k)
		}
		return n, nil
	}
	var err error
	o.Limit, err = parse("limit", DefaultLimit)
	if err != nil {
		return ListOptions{}, err
	}
	o.Offset, err = parse("offset", 0)
	if err != nil {
		return ListOptions{}, err
	}
	if o.Limit < 1 || o.Limit > MaxLimit || o.Offset < 0 {
		return ListOptions{}, fmt.Errorf("%w: limit must be 1..%d and offset nonnegative", ErrInvalid, MaxLimit)
	}
	if v, ok := values["status"]; ok {
		o.Status = v[0]
		if !IsStatus(o.Status) {
			return ListOptions{}, fmt.Errorf("%w: unknown status", ErrInvalid)
		}
	}
	from, hasFrom := values["date_from"]
	to, hasTo := values["date_to"]
	parseDate := func(v string) (time.Time, error) {
		t, e := time.Parse("2006-01-02", v)
		if e != nil || t.Format("2006-01-02") != v {
			return time.Time{}, fmt.Errorf("%w: dates must use YYYY-MM-DD", ErrInvalid)
		}
		return t.UTC(), nil
	}
	if hasFrom {
		o.DateFrom = from[0]
		t, e := parseDate(o.DateFrom)
		if e != nil {
			return ListOptions{}, e
		}
		o.From = t.Format("2006-01-02T15:04:05.000000000Z07:00")
	}
	if hasTo {
		o.DateTo = to[0]
		t, e := parseDate(o.DateTo)
		if e != nil {
			return ListOptions{}, e
		}
		o.ToExclusive = t.AddDate(0, 0, 1).Format("2006-01-02T15:04:05.000000000Z07:00")
	}
	if hasFrom && hasTo && o.DateFrom > o.DateTo {
		return ListOptions{}, fmt.Errorf("%w: date_from must not be after date_to", ErrInvalid)
	}
	return o, nil
}
