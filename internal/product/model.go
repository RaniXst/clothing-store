package product

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

type Product struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Gender      string `json:"gender"`
	Size        string `json:"size"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
	Season      string `json:"season"`
	Price       int64  `json:"price"`
}

type ProductInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Gender      string `json:"gender"`
	Size        string `json:"size"`
	Color       string `json:"color"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
	Season      string `json:"season"`
	Price       int64  `json:"price"`
}

func (in ProductInput) Validate() error {
	for field, value := range map[string]string{
		"name": in.Name, "type": in.Type, "gender": in.Gender,
		"size": in.Size, "color": in.Color, "brand": in.Brand, "season": in.Season,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s cannot be empty", field)
		}
	}
	if in.Price < 0 {
		return fmt.Errorf("price cannot be negative")
	}
	return nil
}

type ListOptions struct {
	Limit  int64
	Offset int64
	Type   string
	Gender string
	Size   string
	Color  string
	Brand  string
	Season string
	Sort   string
}

type ProductPage struct {
	Items  []Product `json:"items"`
	Limit  int64     `json:"limit"`
	Offset int64     `json:"offset"`
	Total  int64     `json:"total"`
}

func ParseListOptions(values url.Values) (ListOptions, error) {
	options := ListOptions{Limit: DefaultLimit}
	allowed := map[string]bool{
		"limit": true, "offset": true, "type": true, "gender": true,
		"size": true, "color": true, "brand": true, "season": true, "sort": true,
	}
	for key := range values {
		if !allowed[key] {
			return ListOptions{}, fmt.Errorf("unknown query parameter %q", key)
		}
		if len(values[key]) != 1 {
			return ListOptions{}, fmt.Errorf("query parameter %q must be provided once", key)
		}
	}

	parseInteger := func(key string, fallback int64) (int64, error) {
		items, ok := values[key]
		if !ok {
			return fallback, nil
		}
		parsed, err := strconv.ParseInt(items[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("query parameter %q must be an integer", key)
		}
		return parsed, nil
	}
	limit, err := parseInteger("limit", DefaultLimit)
	if err != nil {
		return ListOptions{}, err
	}
	if limit < 1 || limit > MaxLimit {
		return ListOptions{}, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}
	offset, err := parseInteger("offset", 0)
	if err != nil {
		return ListOptions{}, err
	}
	if offset < 0 {
		return ListOptions{}, fmt.Errorf("offset cannot be negative")
	}
	options.Limit, options.Offset = limit, offset

	for key, target := range map[string]*string{
		"type": &options.Type, "gender": &options.Gender, "size": &options.Size,
		"color": &options.Color, "brand": &options.Brand, "season": &options.Season,
	} {
		if items, ok := values[key]; ok {
			value := strings.TrimSpace(items[0])
			if value == "" {
				return ListOptions{}, fmt.Errorf("query parameter %q cannot be empty", key)
			}
			*target = value
		}
	}
	if items, ok := values["sort"]; ok {
		options.Sort = items[0]
		if options.Sort != "price_asc" && options.Sort != "price_desc" {
			return ListOptions{}, fmt.Errorf("sort must be price_asc or price_desc")
		}
	}
	return options, nil
}
