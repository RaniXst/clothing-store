package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/middleware"
	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/RaniXst/clothing-store/migrations"
	"github.com/go-chi/chi/v5"
)

type authTestApp struct {
	router    *chi.Mux
	db        *sql.DB
	service   *Service
	users     *user.Service
	logBuffer *bytes.Buffer
}

func newAuthTestApp(t *testing.T, maxLoginAttempts int) authTestApp {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir()+"/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	repository := user.NewSQLRepository(db)
	service, err := NewService(repository, NewSQLSessionRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	users := user.NewService(repository, PasswordHasher{})
	buffer := &bytes.Buffer{}
	router := chi.NewRouter()
	router.Use(middleware.Logging(slog.New(slog.NewTextHandler(buffer, nil))))
	handler := NewHandler(service, NewLoginLimiter(maxLoginAttempts, 15*time.Minute))
	handler.RegisterPublicRoutes(router)
	router.Group(func(protected chi.Router) {
		protected.Use(service.RequireSession)
		handler.RegisterProtectedRoutes(protected)
		protected.With(RequireRoles("admin"), CSRF).Get("/admin-only", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		protected.With(CSRF).Post("/csrf-probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	t.Cleanup(func() { _ = db.Close() })
	return authTestApp{router: router, db: db, service: service, users: users, logBuffer: buffer}
}

func authRequest(app authTestApp, method, path string, body []byte, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	app.router.ServeHTTP(response, request)
	return response
}

func registerJSON(name, email, phone, password string) []byte {
	data, _ := json.Marshal(Registration{Name: name, Email: email, Phone: phone, Password: password})
	return data
}

func loginCookies(t *testing.T, app authTestApp, email, password string) (*http.Cookie, *http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(Credentials{Email: email, Password: password})
	response := authRequest(app, http.MethodPost, "/auth/login", body)
	var session, csrf *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		switch cookie.Name {
		case sessionCookieName:
			session = cookie
		case csrfCookieName:
			csrf = cookie
		}
	}
	return session, csrf, response
}

func TestRegistrationLoginLogoutCSRFAndNoSecretDisclosure(t *testing.T) {
	app := newAuthTestApp(t, 10)
	password := "correct horse battery staple"
	response := authRequest(app, http.MethodPost, "/auth/register", registerJSON("Client User", "Client@Example.com", "+100000000", password))
	if response.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "password_hash") || strings.Contains(response.Body.String(), password) {
		t.Fatalf("registration response contains sensitive data or is cacheable: %s", response.Body.String())
	}
	var result struct {
		User      user.User `json:"user"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User.Role.Name != "client" || result.User.Email != "client@example.com" || result.ExpiresAt.Sub(time.Now()) < SessionLifetime-time.Minute {
		t.Fatalf("unexpected registration result: %+v", result)
	}
	duplicate := authRequest(app, http.MethodPost, "/auth/register", registerJSON("Another Name", "CLIENT@example.com", "+100000099", password))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("case-insensitive duplicate email = %d, want 409", duplicate.Code)
	}
	var session, csrf *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			session = cookie
		}
		if cookie.Name == csrfCookieName {
			csrf = cookie
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.MaxAge != int(SessionLifetime.Seconds()) || csrf == nil || csrf.HttpOnly {
		t.Fatalf("unexpected auth/CSRF cookie attributes: session=%+v csrf=%+v", session, csrf)
	}
	if len(session.Value) < 40 || strings.Contains(response.Body.String(), session.Value) || strings.Contains(response.Body.String(), csrf.Value) {
		t.Fatal("session or CSRF token was returned in JSON or was too short")
	}
	var storedTokenHash string
	if err := app.db.QueryRow(`SELECT token_hash FROM auth_sessions`).Scan(&storedTokenHash); err != nil {
		t.Fatal(err)
	}
	if storedTokenHash == session.Value || len(storedTokenHash) != 64 {
		t.Fatal("database must store only a SHA-256 digest of the session token")
	}
	if got := authRequest(app, http.MethodGet, "/auth/me", nil, session).Code; got != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200", got)
	}
	if got := authRequest(app, http.MethodPost, "/csrf-probe", []byte(`{}`), session).Code; got != http.StatusForbidden {
		t.Fatalf("state-changing request without CSRF token = %d, want 403", got)
	}
	probe := httptest.NewRequest(http.MethodPost, "/csrf-probe", strings.NewReader(`{}`))
	probe.AddCookie(session)
	probe.AddCookie(csrf)
	probe.Header.Set("X-CSRF-Token", csrf.Value)
	probeResponse := httptest.NewRecorder()
	app.router.ServeHTTP(probeResponse, probe)
	if probeResponse.Code != http.StatusNoContent {
		t.Fatalf("state-changing request with CSRF token = %d, want 204", probeResponse.Code)
	}
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.AddCookie(session)
	logout.AddCookie(csrf)
	logout.Header.Set("X-CSRF-Token", csrf.Value)
	logoutResponse := httptest.NewRecorder()
	app.router.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	if got := authRequest(app, http.MethodGet, "/auth/me", nil, session).Code; got != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", got)
	}
	if strings.Contains(app.logBuffer.String(), password) || strings.Contains(app.logBuffer.String(), session.Value) || strings.Contains(app.logBuffer.String(), csrf.Value) {
		t.Fatal("password or token appeared in HTTP logs")
	}
}

func TestLoginGenericFailureRateLimitAndCurrentRole(t *testing.T) {
	app := newAuthTestApp(t, 10)
	password := "another correct horse battery"
	if _, err := app.users.BootstrapAdmin(context.Background(), "Admin", "admin@example.com", "+100000001", password); err != nil {
		t.Fatal(err)
	}
	if _, err := app.users.Register(context.Background(), "Client", "client@example.com", "+100000002", password); err != nil {
		t.Fatal(err)
	}
	unknown := authRequest(app, http.MethodPost, "/auth/login", []byte(`{"email":"missing@example.com","password":"wrong password"}`))
	wrong := authRequest(app, http.MethodPost, "/auth/login", []byte(`{"email":"client@example.com","password":"wrong password"}`))
	if unknown.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized || unknown.Body.String() != wrong.Body.String() {
		t.Fatalf("login errors differ: unknown=%d %s wrong=%d %s", unknown.Code, unknown.Body.String(), wrong.Code, wrong.Body.String())
	}
	session, _, good := loginCookies(t, app, "ADMIN@example.com", password)
	if good.Code != http.StatusOK || session == nil {
		t.Fatalf("valid login failed: %d %s", good.Code, good.Body.String())
	}
	secureReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"another correct horse battery"}`))
	secureReq.Header.Set("Content-Type", "application/json")
	secureReq.TLS = &tls.ConnectionState{}
	secureResponse := httptest.NewRecorder()
	app.router.ServeHTTP(secureResponse, secureReq)
	secureFound := false
	for _, cookie := range secureResponse.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Secure {
			secureFound = true
		}
	}
	if !secureFound {
		t.Fatal("session cookie must be Secure on HTTPS requests")
	}
	if got := authRequest(app, http.MethodGet, "/admin-only", nil, session).Code; got != http.StatusNoContent {
		t.Fatalf("admin access = %d, want 204", got)
	}
	clientSession, _, clientLogin := loginCookies(t, app, "client@example.com", password)
	if clientLogin.Code != http.StatusOK || clientSession == nil {
		t.Fatalf("client login failed: %d", clientLogin.Code)
	}
	if got := authRequest(app, http.MethodGet, "/admin-only", nil, clientSession).Code; got != http.StatusForbidden {
		t.Fatalf("client admin access = %d, want 403", got)
	}
	if _, err := app.db.Exec(`UPDATE users SET role_id=? WHERE email=?`, user.AdminRoleID, "client@example.com"); err != nil {
		t.Fatal(err)
	}
	if got := authRequest(app, http.MethodGet, "/admin-only", nil, clientSession).Code; got != http.StatusNoContent {
		t.Fatalf("updated role was not loaded from database: status=%d", got)
	}

	limited := newAuthTestApp(t, 1)
	first := authRequest(limited, http.MethodPost, "/auth/login", []byte(`{"email":"nobody@example.com","password":"wrong password"}`))
	second := authRequest(limited, http.MethodPost, "/auth/login", []byte(`{"email":"nobody@example.com","password":"wrong password"}`))
	if first.Code != http.StatusUnauthorized || second.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit responses = %d then %d, want 401 then 429", first.Code, second.Code)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	app := newAuthTestApp(t, 10)
	password := "expired test session password"
	if _, err := app.users.Register(context.Background(), "Client", "expired@example.com", "+100000003", password); err != nil {
		t.Fatal(err)
	}
	session, _, response := loginCookies(t, app, "expired@example.com", password)
	if response.Code != http.StatusOK || session == nil {
		t.Fatalf("login failed: %d", response.Code)
	}
	if _, err := app.db.Exec(`UPDATE auth_sessions SET expires_at=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if got := authRequest(app, http.MethodGet, "/auth/me", nil, session).Code; got != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d, want 401", got)
	}
}

func TestRegistrationCannotChooseRole(t *testing.T) {
	app := newAuthTestApp(t, 10)
	response := authRequest(app, http.MethodPost, "/auth/register", []byte(`{"name":"Admin","email":"x@example.com","phone":"+100000000","password":"long enough password","role_id":"33333333-3333-4333-8333-333333333333"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("registration with role_id = %d, want 400", response.Code)
	}
	var count int
	if err := app.db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invalid registration created an account")
	}
}
