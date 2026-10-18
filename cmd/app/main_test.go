package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/middleware"
	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/RaniXst/clothing-store/migrations"
)

func TestHealthEndpoint(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("check database: %v", err)
	}

	logger := testLogger()
	router := chi.NewRouter()
	router.Use(middleware.Logging(logger))

	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStageThreeRouteAuthorization(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	router, err := buildRouter(ctx, testLogger(), db, filepath.Join(t.TempDir(), "uploads"), false)
	if err != nil {
		t.Fatal(err)
	}
	repository := user.NewSQLRepository(db)
	users := user.NewService(repository, auth.PasswordHasher{})
	if _, err := users.BootstrapAdmin(ctx, "Admin", "admin@example.com", "+100000000", "admin route test password"); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Register(ctx, "Client", "client@example.com", "+100000001", "client route test password"); err != nil {
		t.Fatal(err)
	}
	authService, err := auth.NewService(repository, auth.NewSQLSessionRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	login := func(email, password string) (*http.Cookie, *http.Cookie) {
		t.Helper()
		issued, err := authService.Login(ctx, auth.Credentials{Email: email, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: "clothing_session", Value: issued.Token}, &http.Cookie{Name: "clothing_csrf", Value: issued.CSRFToken}
	}
	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, "/product", nil))
	if guest.Code != http.StatusOK {
		t.Fatalf("guest catalog read = %d, want 200", guest.Code)
	}
	guestWrite := httptest.NewRecorder()
	guestBody := []byte(`{"name":"Test","type":"shirt","gender":"unisex","size":"M","color":"black","description":"","brand":"Test","season":"summer","price":100}`)
	guestRequest := httptest.NewRequest(http.MethodPost, "/product", bytes.NewReader(guestBody))
	guestRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(guestWrite, guestRequest)
	if guestWrite.Code != http.StatusUnauthorized {
		t.Fatalf("guest catalog write = %d, want 401", guestWrite.Code)
	}
	clientSession, clientCSRF := login("client@example.com", "client route test password")
	clientRequest := httptest.NewRequest(http.MethodPost, "/product", bytes.NewReader(guestBody))
	clientRequest.Header.Set("Content-Type", "application/json")
	clientRequest.AddCookie(clientSession)
	clientRequest.AddCookie(clientCSRF)
	clientRequest.Header.Set("X-CSRF-Token", clientCSRF.Value)
	clientResponse := httptest.NewRecorder()
	router.ServeHTTP(clientResponse, clientRequest)
	if clientResponse.Code != http.StatusForbidden {
		t.Fatalf("client catalog write = %d, want 403", clientResponse.Code)
	}
	adminSession, adminCSRF := login("admin@example.com", "admin route test password")
	adminRequest := httptest.NewRequest(http.MethodPost, "/product", bytes.NewReader(guestBody))
	adminRequest.Header.Set("Content-Type", "application/json")
	adminRequest.AddCookie(adminSession)
	adminRequest.AddCookie(adminCSRF)
	adminRequest.Header.Set("X-CSRF-Token", adminCSRF.Value)
	adminResponse := httptest.NewRecorder()
	router.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusCreated {
		t.Fatalf("admin catalog write = %d: %s", adminResponse.Code, adminResponse.Body.String())
	}
	photoWrite := httptest.NewRecorder()
	photoRequest := httptest.NewRequest(http.MethodPost, "/photo", strings.NewReader(""))
	photoRequest.Header.Set("Content-Type", "multipart/form-data; boundary=empty")
	router.ServeHTTP(photoWrite, photoRequest)
	if photoWrite.Code != http.StatusUnauthorized {
		t.Fatalf("guest photo upload = %d, want 401", photoWrite.Code)
	}
}
