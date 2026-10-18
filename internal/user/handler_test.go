package user_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/RaniXst/clothing-store/migrations"
	"github.com/go-chi/chi/v5"
)

func newUserTestApp(t *testing.T) (*chi.Mux, *sql.DB, *http.Cookie, *http.Cookie, *user.Service) {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir()+"/users.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repository := user.NewSQLRepository(db)
	service := user.NewService(repository, auth.PasswordHasher{})
	authService, err := auth.NewService(repository, auth.NewSQLSessionRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	handler := user.NewHandler(service)
	authHandler := auth.NewHandler(authService, auth.NewLoginLimiter(20, time.Minute))
	router := chi.NewRouter()
	handler.RegisterPublicRoutes(router)
	authHandler.RegisterPublicRoutes(router)
	router.Group(func(protected chi.Router) {
		protected.Use(authService.RequireSession)
		authHandler.RegisterProtectedRoutes(protected)
		protected.Group(func(admin chi.Router) {
			admin.Use(auth.RequireRoles("admin"))
			admin.Use(auth.CSRF)
			handler.RegisterAdminRoutes(admin)
			admin.Get("/admin-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		})
	})
	if _, err := service.BootstrapAdmin(context.Background(), "Admin", "admin@example.com", "+100000000", "administrator test password"); err != nil {
		t.Fatal(err)
	}
	loginBody := []byte(`{"email":"admin@example.com","password":"administrator test password"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("admin login = %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
	var session, csrf *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == "clothing_session" {
			session = cookie
		}
		if cookie.Name == "clothing_csrf" {
			csrf = cookie
		}
	}
	if session == nil || csrf == nil {
		t.Fatal("login did not set required cookies")
	}
	t.Cleanup(func() { _ = db.Close() })
	return router, db, session, csrf, service
}

func userRequest(router http.Handler, method, path string, body []byte, session, csrf *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if session != nil {
		request.AddCookie(session)
	}
	if csrf != nil {
		request.AddCookie(csrf)
		request.Header.Set("X-CSRF-Token", csrf.Value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestUserAndRoleCRUDAndAuthorization(t *testing.T) {
	router, db, adminSession, adminCSRF, _ := newUserTestApp(t)
	if got := userRequest(router, http.MethodGet, "/role", nil, nil, nil).Code; got != http.StatusOK {
		t.Fatalf("public role list = %d, want 200", got)
	}
	if got := userRequest(router, http.MethodPost, "/user", []byte(`{}`), nil, nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("guest user creation = %d, want 401", got)
	}
	var adminID string
	if err := db.QueryRow(`SELECT id FROM users WHERE email='admin@example.com'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	adminDemotion := []byte(`{"name":"Admin","email":"admin@example.com","phone":"+100000000","role_id":"11111111-1111-4111-8111-111111111111"}`)
	if got := userRequest(router, http.MethodPut, "/user/"+adminID, adminDemotion, adminSession, adminCSRF).Code; got != http.StatusConflict {
		t.Fatalf("demote last admin = %d, want 409", got)
	}
	if got := userRequest(router, http.MethodDelete, "/user/"+adminID, nil, adminSession, adminCSRF).Code; got != http.StatusConflict {
		t.Fatalf("delete last admin = %d, want 409", got)
	}

	createBody := []byte(`{"name":"Client One","email":"client@example.com","phone":"+100000001","role_id":"11111111-1111-4111-8111-111111111111","password":"client test password"}`)
	created := userRequest(router, http.MethodPost, "/user", createBody, adminSession, adminCSRF)
	if created.Code != http.StatusCreated || bytes.Contains(created.Body.Bytes(), []byte("password_hash")) || bytes.Contains(created.Body.Bytes(), []byte("client test password")) {
		t.Fatalf("user create = %d: %s", created.Code, created.Body.String())
	}
	var createdUser user.User
	if err := json.Unmarshal(created.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}
	page := userRequest(router, http.MethodGet, "/user?limit=1&offset=0", nil, adminSession, nil)
	if page.Code != http.StatusOK || bytes.Contains(page.Body.Bytes(), []byte("password_hash")) {
		t.Fatalf("user list = %d: %s", page.Code, page.Body.String())
	}
	byRole := userRequest(router, http.MethodGet, "/user/role/11111111-1111-4111-8111-111111111111", nil, adminSession, nil)
	if byRole.Code != http.StatusOK {
		t.Fatalf("list users by role = %d: %s", byRole.Code, byRole.Body.String())
	}
	update := []byte(`{"name":"Client Updated","email":"CLIENT@example.com","phone":"+100000009","role_id":"11111111-1111-4111-8111-111111111111"}`)
	updated := userRequest(router, http.MethodPut, "/user/"+createdUser.ID, update, adminSession, adminCSRF)
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte("client updated")) && !bytes.Contains(updated.Body.Bytes(), []byte("Client Updated")) {
		t.Fatalf("user update = %d: %s", updated.Code, updated.Body.String())
	}
	deleted := userRequest(router, http.MethodDelete, "/user/"+createdUser.ID, nil, adminSession, adminCSRF)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("user delete = %d: %s", deleted.Code, deleted.Body.String())
	}

	createdRole := userRequest(router, http.MethodPost, "/role", []byte(`{"name":"auditor"}`), adminSession, adminCSRF)
	if createdRole.Code != http.StatusCreated {
		t.Fatalf("create role = %d: %s", createdRole.Code, createdRole.Body.String())
	}
	var role user.Role
	if err := json.Unmarshal(createdRole.Body.Bytes(), &role); err != nil {
		t.Fatal(err)
	}
	updatedRole := userRequest(router, http.MethodPut, "/role/"+role.ID, []byte(`{"name":"reviewer"}`), adminSession, adminCSRF)
	if updatedRole.Code != http.StatusOK {
		t.Fatalf("update custom role = %d: %s", updatedRole.Code, updatedRole.Body.String())
	}
	if got := userRequest(router, http.MethodPut, "/role/"+user.AdminRoleID, []byte(`{"name":"superadmin"}`), adminSession, adminCSRF).Code; got != http.StatusConflict {
		t.Fatalf("rename system role = %d, want 409", got)
	}
	if got := userRequest(router, http.MethodDelete, "/role/"+user.ClientRoleID, nil, adminSession, adminCSRF).Code; got != http.StatusConflict {
		t.Fatalf("delete system role = %d, want 409", got)
	}
	customUserBody := []byte(`{"name":"Custom Role User","email":"custom@example.com","phone":"+100000008","role_id":"` + role.ID + `","password":"custom user password"}`)
	customUserResponse := userRequest(router, http.MethodPost, "/user", customUserBody, adminSession, adminCSRF)
	if customUserResponse.Code != http.StatusCreated {
		t.Fatalf("create user with custom role = %d: %s", customUserResponse.Code, customUserResponse.Body.String())
	}
	var customUser user.User
	if err := json.Unmarshal(customUserResponse.Body.Bytes(), &customUser); err != nil {
		t.Fatal(err)
	}
	customLogin := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte(`{"email":"custom@example.com","password":"custom user password"}`)))
	customLogin.Header.Set("Content-Type", "application/json")
	customLoginResponse := httptest.NewRecorder()
	router.ServeHTTP(customLoginResponse, customLogin)
	var customSession *http.Cookie
	for _, cookie := range customLoginResponse.Result().Cookies() {
		if cookie.Name == "clothing_session" {
			customSession = cookie
		}
	}
	if customLoginResponse.Code != http.StatusOK || customSession == nil {
		t.Fatalf("custom role login = %d: %s", customLoginResponse.Code, customLoginResponse.Body.String())
	}
	if got := userRequest(router, http.MethodGet, "/admin-only", nil, customSession, nil).Code; got != http.StatusForbidden {
		t.Fatalf("custom role admin access = %d, want 403", got)
	}
	if got := userRequest(router, http.MethodDelete, "/role/"+role.ID, nil, adminSession, adminCSRF).Code; got != http.StatusConflict {
		t.Fatalf("delete assigned custom role = %d, want 409", got)
	}
	moveBack := []byte(`{"name":"Custom Role User","email":"custom@example.com","phone":"+100000008","role_id":"` + user.ClientRoleID + `"}`)
	if got := userRequest(router, http.MethodPut, "/user/"+customUser.ID, moveBack, adminSession, adminCSRF).Code; got != http.StatusOK {
		t.Fatalf("move user back to client role = %d, want 200", got)
	}
	if got := userRequest(router, http.MethodDelete, "/role/"+role.ID, nil, adminSession, adminCSRF).Code; got != http.StatusNoContent {
		t.Fatalf("delete custom role = %d, want 204", got)
	}
	if got := userRequest(router, http.MethodGet, "/user?limit=1000", nil, adminSession, nil).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid user pagination = %d, want 400", got)
	}
}

func TestUserWithOrderHistoryCannotBeDeleted(t *testing.T) {
	router, db, adminSession, adminCSRF, service := newUserTestApp(t)
	created, err := service.Register(context.Background(), "Ordered User", "ordered@example.com", "+100000004", "ordered user long password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO orders (id,user_id,datetime,status) VALUES ('order-1',?,'2026-10-18T00:00:00Z','Принят')`, created.ID); err != nil {
		t.Fatal(err)
	}
	response := userRequest(router, http.MethodDelete, "/user/"+created.ID, nil, adminSession, adminCSRF)
	if response.Code != http.StatusConflict {
		t.Fatalf("delete user with orders = %d: %s", response.Code, response.Body.String())
	}
	if _, err := service.Get(context.Background(), created.ID); err != nil {
		t.Fatalf("user with order history was removed: %v", err)
	}
}
