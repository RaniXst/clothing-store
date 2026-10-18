package photo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	_ "image/jpeg"
	_ "image/png"

	"github.com/RaniXst/clothing-store/internal/product"
	"github.com/RaniXst/clothing-store/internal/storage"
)

const (
	MaxUploadSize = 5 << 20
	MaxPixels     = 25_000_000
)

var (
	ErrInvalid       = errors.New("invalid photo")
	ErrNotFound      = errors.New("photo not found")
	ErrProductAbsent = errors.New("product not found")
)

type Repository interface {
	Create(context.Context, Photo) error
	Get(context.Context, string) (Photo, error)
	Delete(context.Context, string) error
	ListFilenamesForProduct(context.Context, string) ([]string, error)
	ListFilenames(context.Context) ([]string, error)
}

type ProductLookup interface {
	Get(context.Context, string) (product.Product, error)
}

type Service struct {
	repository Repository
	products   ProductLookup
	files      *storage.Local
	mutationMu *sync.Mutex
}

func NewService(repository Repository, products ProductLookup, files *storage.Local, mutationMu *sync.Mutex) *Service {
	if mutationMu == nil {
		mutationMu = &sync.Mutex{}
	}
	return &Service{repository: repository, products: products, files: files, mutationMu: mutationMu}
}

func (s *Service) Create(ctx context.Context, productID string, data []byte) (Photo, error) {
	parsedProductID, err := uuid.Parse(productID)
	if err != nil {
		return Photo{}, fmt.Errorf("%w: product_id must be a UUID", ErrInvalid)
	}
	if len(data) == 0 || len(data) > MaxUploadSize {
		return Photo{}, fmt.Errorf("%w: image must be between 1 byte and %d bytes", ErrInvalid, MaxUploadSize)
	}
	ext, _, err := validateImage(data)
	if err != nil {
		return Photo{}, err
	}

	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	canonicalProductID := parsedProductID.String()
	if _, err := s.products.Get(ctx, canonicalProductID); err != nil {
		if errors.Is(err, product.ErrNotFound) {
			return Photo{}, ErrProductAbsent
		}
		return Photo{}, err
	}
	item := Photo{ID: uuid.NewString(), Filename: uuid.NewString() + ext, ProductID: canonicalProductID}
	tempName, err := s.files.Prepare(data)
	if err != nil {
		return Photo{}, err
	}
	defer func() { _ = s.files.RemoveTemp(tempName) }()
	if err := s.files.Publish(tempName, item.Filename); err != nil {
		return Photo{}, err
	}
	if err := s.repository.Create(ctx, item); err != nil {
		if removeErr := s.files.Remove(item.Filename); removeErr != nil {
			slog.Error("failed to remove unreferenced uploaded image", "filename", item.Filename, "error", removeErr)
		}
		return Photo{}, err
	}
	return item, nil
}

func (s *Service) Get(ctx context.Context, id string) (Photo, []byte, string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Photo{}, nil, "", fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	item, err := s.repository.Get(ctx, parsed.String())
	if err != nil {
		return Photo{}, nil, "", err
	}
	data, err := s.files.Read(item.Filename)
	if err != nil {
		return Photo{}, nil, "", fmt.Errorf("read stored photo: %w", err)
	}
	contentType := mime.TypeByExtension(filepath.Ext(item.Filename))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return item, data, contentType, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: invalid UUID", ErrInvalid)
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	item, err := s.repository.Get(ctx, parsed.String())
	if err != nil {
		return err
	}
	staged, err := s.files.StageDelete([]string{item.Filename})
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, item.ID); err != nil {
		if restoreErr := s.files.Restore(staged); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore photo: %w", restoreErr))
		}
		return err
	}
	if err := s.files.Purge(staged); err != nil {
		slog.Warn("photo deleted; file cleanup deferred", "filename", item.Filename, "error", err)
	}
	return nil
}

func (s *Service) Recover(ctx context.Context) error {
	filenames, err := s.repository.ListFilenames(ctx)
	if err != nil {
		return fmt.Errorf("list photo files for recovery: %w", err)
	}
	referenced := make(map[string]struct{}, len(filenames))
	for _, filename := range filenames {
		referenced[filename] = struct{}{}
	}
	return s.files.Recover(referenced)
}

func validateImage(data []byte) (string, string, error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("%w: empty image", ErrInvalid)
	}
	contentType := http.DetectContentType(data[:min(len(data), 512)])
	var extension string
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	default:
		return "", "", fmt.Errorf("%w: only JPEG and PNG images are supported", ErrInvalid)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return "", "", fmt.Errorf("%w: malformed image", ErrInvalid)
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxPixels {
		return "", "", fmt.Errorf("%w: image dimensions exceed %d megapixels", ErrInvalid, MaxPixels/1_000_000)
	}
	if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || (format != "jpeg" && format != "png") {
		return "", "", fmt.Errorf("%w: malformed image", ErrInvalid)
	}
	return extension, contentType, nil
}
