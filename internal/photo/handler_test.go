package photo_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/photo"
	"github.com/RaniXst/clothing-store/internal/product"
	"github.com/RaniXst/clothing-store/internal/storage"
	"github.com/RaniXst/clothing-store/migrations"
)

type photoTestApp struct {
	router    *chi.Mux
	db        *sql.DB
	files     *storage.Local
	productID string
}

func newPhotoTestApp(t *testing.T) photoTestApp {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	productRepo := product.NewSQLRepository(db)
	photoRepo := photo.NewSQLRepository(db)
	lock := &sync.Mutex{}
	productService := product.NewService(productRepo, photoRepo, files, lock)
	photoService := photo.NewService(photoRepo, productRepo, files, lock)
	item, err := productService.Create(context.Background(), product.ProductInput{
		Name: "T-shirt", Type: "top", Gender: "unisex", Size: "M", Color: "blue",
		Description: "cotton", Brand: "Acme", Season: "summer", Price: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	photo.NewHandler(photoService).RegisterRoutes(router)
	t.Cleanup(func() { _ = db.Close() })
	return photoTestApp{router: router, db: db, files: files, productID: item.ID}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func uploadRequest(t *testing.T, app photoTestApp, data []byte, productID string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("product_id", productID); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "user-supplied-name.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/photo", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	app.router.ServeHTTP(recorder, request)
	return recorder
}

func TestPhotoUploadGetDelete(t *testing.T) {
	app := newPhotoTestApp(t)
	data := pngBytes(t)
	created := uploadRequest(t, app, data, app.productID)
	if created.Code != http.StatusCreated {
		t.Fatalf("POST /photo = %d, body %s", created.Code, created.Body.String())
	}
	var item photo.Photo
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(item.ID); err != nil {
		t.Fatalf("photo id is not UUID: %v", err)
	}
	if item.ProductID != app.productID || filepath.Ext(item.Filename) != ".png" || item.Filename == "user-supplied-name.png" {
		t.Fatalf("unexpected photo metadata: %+v", item)
	}
	if _, err := app.files.Read(item.Filename); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}

	get := httptest.NewRecorder()
	app.router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/photo/"+item.ID, nil))
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "image/png" || !bytes.Equal(get.Body.Bytes(), data) {
		t.Fatalf("GET /photo/{id}: status=%d content-type=%q", get.Code, get.Header().Get("Content-Type"))
	}
	if get.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("GET response lacks nosniff header")
	}

	deleted := httptest.NewRecorder()
	app.router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/photo/"+item.ID, nil))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DELETE /photo/{id} = %d, body %s", deleted.Code, deleted.Body.String())
	}
	if _, err := app.files.Read(item.Filename); err == nil {
		t.Fatal("photo file remains after successful delete")
	}
	var remaining int
	if err := app.db.QueryRow("SELECT count(*) FROM photos WHERE id = ?", item.ID).Scan(&remaining); err != nil {
		t.Fatalf("database delete check: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("photo metadata remains after delete: %d rows", remaining)
	}
}

func TestPhotoRejectsInvalidAndOversizedUploads(t *testing.T) {
	app := newPhotoTestApp(t)
	invalid := uploadRequest(t, app, []byte("not an image"), app.productID)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid image status = %d, want 400", invalid.Code)
	}
	missingProduct := uploadRequest(t, app, pngBytes(t), uuid.NewString())
	if missingProduct.Code != http.StatusNotFound {
		t.Fatalf("unknown product status = %d, want 404", missingProduct.Code)
	}
	oversized := uploadRequest(t, app, bytes.Repeat([]byte("x"), photo.MaxUploadSize+1), app.productID)
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized image status = %d, want 413", oversized.Code)
	}
}
