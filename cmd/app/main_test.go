package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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

func TestStageFiveRouteAuthorizationAndCartOrderFlow(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "stage5-routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	logger := testLogger()
	router, err := buildRouter(ctx, logger, db, filepath.Join(t.TempDir(), "uploads"), false)
	if err != nil {
		t.Fatal(err)
	}
	userRepo := user.NewSQLRepository(db)
	userService := user.NewService(userRepo, auth.PasswordHasher{})
	if _, err := userService.BootstrapAdmin(ctx, "Admin", "stage5-admin@example.test", "+10000000000", "stage5 admin password"); err != nil {
		t.Fatal(err)
	}
	authService, err := auth.NewService(userRepo, auth.NewSQLSessionRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	client, err := authService.Register(ctx, auth.Registration{Name: "Client", Email: "stage5-client@example.test", Phone: "+10000000001", Password: "stage5 client password"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := authService.Register(ctx, auth.Registration{Name: "Other", Email: "stage5-other@example.test", Phone: "+10000000002", Password: "stage5 other password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userService.Create(ctx, user.CreateInput{Name: "Manager", Email: "stage5-manager@example.test", Phone: "+10000000003", RoleID: user.ManagerRoleID, Password: "stage5 manager password"}); err != nil {
		t.Fatal(err)
	}
	manager, err := authService.Login(ctx, auth.Credentials{Email: "stage5-manager@example.test", Password: "stage5 manager password"})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := authService.Login(ctx, auth.Credentials{Email: "stage5-admin@example.test", Password: "stage5 admin password"})
	if err != nil {
		t.Fatal(err)
	}
	productID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := db.ExecContext(ctx, `INSERT INTO products(id,name,type,gender,size,color,description,brand,season,price) VALUES(?,?,?,?,?,?,?,?,?,?)`, productID, "Shirt", "top", "unisex", "M", "blue", "", "Brand", "summer", 12500); err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, session auth.IssuedSession, body string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if session.Token != "" {
			req.AddCookie(&http.Cookie{Name: "clothing_session", Value: session.Token})
			req.AddCookie(&http.Cookie{Name: "clothing_csrf", Value: session.CSRFToken})
		}
		if csrf {
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if got := request(http.MethodGet, "/cart/"+client.User.ID, auth.IssuedSession{}, "", false).Code; got != http.StatusUnauthorized {
		t.Fatalf("guest cart GET=%d", got)
	}
	noCSRF := request(http.MethodPost, "/cart", client, `{"product_id":"`+productID+`","count":2}`, false)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("cart POST without CSRF=%d", noCSRF.Code)
	}
	added := request(http.MethodPost, "/cart", client, `{"product_id":"`+productID+`","count":2}`, true)
	if added.Code != http.StatusCreated {
		t.Fatalf("cart POST=%d: %s", added.Code, added.Body.String())
	}
	var cartItem struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(added.Body.Bytes(), &cartItem); err != nil {
		t.Fatal(err)
	}
	if got := request(http.MethodGet, "/cart/"+client.User.ID, client, "", false).Code; got != http.StatusOK {
		t.Fatalf("owner cart GET=%d", got)
	}
	if got := request(http.MethodPut, "/cart/"+cartItem.ID, client, `{"count":0}`, true).Code; got != http.StatusBadRequest {
		t.Fatalf("cart zero count PUT=%d", got)
	}
	if got := request(http.MethodPut, "/cart/"+cartItem.ID, client, `{"count":3}`, true).Code; got != http.StatusOK {
		t.Fatalf("owner cart PUT=%d", got)
	}
	if got := request(http.MethodGet, "/cart/"+client.User.ID, other, "", false).Code; got != http.StatusNotFound {
		t.Fatalf("foreign cart GET=%d", got)
	}
	if got := request(http.MethodDelete, "/cart/"+cartItem.ID, client, "", true).Code; got != http.StatusNoContent {
		t.Fatalf("owner cart DELETE=%d", got)
	}
	if got := request(http.MethodPost, "/cart", client, `{"product_id":"`+productID+`","count":2}`, true).Code; got != http.StatusCreated {
		t.Fatalf("cart re-add=%d", got)
	}
	created := request(http.MethodPost, "/order", client, "", true)
	if created.Code != http.StatusCreated {
		t.Fatalf("order POST=%d: %s", created.Code, created.Body.String())
	}
	var result struct {
		ID    string `json:"id"`
		Items []struct {
			Price int64 `json:"price"`
		} `json:"items"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Price != 12500 {
		t.Fatalf("checkout response=%+v", result)
	}
	if got := request(http.MethodPost, "/order", client, "", true).Code; got != http.StatusConflict {
		t.Fatalf("repeat checkout=%d", got)
	}
	clientList := request(http.MethodGet, "/order", client, "", false)
	managerList := request(http.MethodGet, "/order", manager, "", false)
	if clientList.Code != 200 || managerList.Code != 200 {
		t.Fatalf("client list=%d manager list=%d", clientList.Code, managerList.Code)
	}
	if got := request(http.MethodGet, "/order?limit=101", client, "", false).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid order pagination=%d", got)
	}
	if got := request(http.MethodGet, "/order/"+result.ID, other, "", false).Code; got != http.StatusNotFound {
		t.Fatalf("foreign order GET=%d", got)
	}
	if got := request(http.MethodGet, "/order_item/"+result.ID, other, "", false).Code; got != http.StatusNotFound {
		t.Fatalf("foreign order items GET=%d", got)
	}
	staffLine := request(http.MethodPost, "/order_item", admin, `{"order_id":"`+result.ID+`","product_id":"`+productID+`","count":1}`, true)
	if staffLine.Code != http.StatusCreated {
		t.Fatalf("admin add order item=%d: %s", staffLine.Code, staffLine.Body.String())
	}
	if got := request(http.MethodGet, "/order_item/"+result.ID, client, "", false).Code; got != http.StatusOK {
		t.Fatalf("owner order items GET=%d", got)
	}
	cancel := request(http.MethodPut, "/order/"+result.ID, client, `{"status":"Отменён"}`, true)
	if cancel.Code != 200 {
		t.Fatalf("client cancel=%d: %s", cancel.Code, cancel.Body.String())
	}
	if got := request(http.MethodPut, "/order/"+result.ID, manager, `{"status":"В работе"}`, true).Code; got != http.StatusConflict {
		t.Fatalf("invalid manager transition=%d", got)
	}
	addedLine := request(http.MethodPost, "/order_item", client, `{"order_id":"`+result.ID+`","product_id":"`+productID+`","count":1}`, true)
	if addedLine.Code != http.StatusForbidden {
		t.Fatalf("client add order item=%d", addedLine.Code)
	}
	staffLine = request(http.MethodPost, "/order_item", admin, `{"order_id":"`+result.ID+`","product_id":"`+productID+`","count":1}`, true)
	if staffLine.Code != http.StatusConflict {
		t.Fatalf("staff add item to cancelled order=%d", staffLine.Code)
	}
}
