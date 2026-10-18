package product

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/RaniXst/clothing-store/internal/storage"
)

var (
	ErrInvalid  = errors.New("invalid product")
	ErrNotFound = errors.New("product not found")
	ErrConflict = errors.New("product is referenced by order history")
)

type Repository interface {
	Create(context.Context, Product) error
	Get(context.Context, string) (Product, error)
	Update(context.Context, string, ProductInput) (Product, error)
	Delete(context.Context, string) error
	List(context.Context, ListOptions) (ProductPage, error)
}

type PhotoFiles interface {
	ListFilenamesForProduct(context.Context, string) ([]string, error)
}

type Service struct {
	repository Repository
	photos     PhotoFiles
	files      *storage.Local
	mutationMu *sync.Mutex
}

func NewService(repository Repository, photos PhotoFiles, files *storage.Local, mutationMu *sync.Mutex) *Service {
	if mutationMu == nil {
		mutationMu = &sync.Mutex{}
	}
	return &Service{repository: repository, photos: photos, files: files, mutationMu: mutationMu}
}

func (s *Service) Create(ctx context.Context, input ProductInput) (Product, error) {
	if err := input.Validate(); err != nil {
		return Product{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	item := Product{
		ID: uuid.NewString(), Name: input.Name, Type: input.Type, Gender: input.Gender,
		Size: input.Size, Color: input.Color, Description: input.Description,
		Brand: input.Brand, Season: input.Season, Price: input.Price,
	}
	if err := s.repository.Create(ctx, item); err != nil {
		return Product{}, fmt.Errorf("create product: %w", err)
	}
	return item, nil
}

func (s *Service) Get(ctx context.Context, id string) (Product, error) {
	canonical, err := normalizeID(id)
	if err != nil {
		return Product{}, err
	}
	item, err := s.repository.Get(ctx, canonical)
	if err != nil {
		return Product{}, err
	}
	return item, nil
}

func (s *Service) Update(ctx context.Context, id string, input ProductInput) (Product, error) {
	canonical, err := normalizeID(id)
	if err != nil {
		return Product{}, err
	}
	if err := input.Validate(); err != nil {
		return Product{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return s.repository.Update(ctx, canonical, input)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	canonical, err := normalizeID(id)
	if err != nil {
		return err
	}

	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	filenames, err := s.photos.ListFilenamesForProduct(ctx, canonical)
	if err != nil {
		return fmt.Errorf("list product photos: %w", err)
	}
	staged, err := s.files.StageDelete(filenames)
	if err != nil {
		return fmt.Errorf("stage product photo deletion: %w", err)
	}
	if err := s.repository.Delete(ctx, canonical); err != nil {
		if restoreErr := s.files.Restore(staged); restoreErr != nil {
			return errors.Join(fmt.Errorf("delete product: %w", err), fmt.Errorf("restore product photos: %w", restoreErr))
		}
		return err
	}
	if err := s.files.Purge(staged); err != nil {
		// The database commit already succeeded; startup recovery will finish cleanup.
		slog.Warn("product deleted; photo cleanup deferred", "error", err)
	}
	return nil
}

func (s *Service) List(ctx context.Context, options ListOptions) (ProductPage, error) {
	return s.repository.List(ctx, options)
}

func normalizeID(value string) (string, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	return id.String(), nil
}
