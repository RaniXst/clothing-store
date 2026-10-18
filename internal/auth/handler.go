package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/go-chi/chi/v5"
)

const (
	sessionCookieName = "clothing_session"
	csrfCookieName    = "clothing_csrf"
	maxAuthBody       = 16 << 10
)

type Handler struct {
	service           *Service
	limiter           *LoginLimiter
	forceSecureCookie bool
}

func NewHandler(service *Service, limiter *LoginLimiter, forceSecureCookie ...bool) *Handler {
	handler := &Handler{service: service, limiter: limiter}
	if len(forceSecureCookie) > 0 {
		handler.forceSecureCookie = forceSecureCookie[0]
	}
	return handler
}

func (h *Handler) RegisterPublicRoutes(router chi.Router) {
	router.Post("/auth/register", h.register)
	router.Post("/auth/login", h.login)
}

func (h *Handler) RegisterProtectedRoutes(router chi.Router) {
	router.Get("/auth/me", h.me)
	router.With(CSRF).Post("/auth/logout", h.logout)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var input Registration
	if err := decodeAuthJSON(w, r, &input); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issued, err := h.service.Register(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, user.ErrInvalid):
			writeAuthError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, user.ErrConflict):
			writeAuthError(w, http.StatusConflict, "email is already registered")
		default:
			writeAuthError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	h.setSessionCookies(w, r, issued)
	noStore(w)
	w.Header().Set("Location", "/auth/me")
	writeJSON(w, http.StatusCreated, map[string]any{"user": issued.User, "expires_at": issued.ExpiresAt})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input Credentials
	if err := decodeAuthJSON(w, r, &input); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.limiter.Allow(remoteIP(r)) {
		noStore(w)
		writeAuthError(w, http.StatusTooManyRequests, "too many login attempts; try again later")
		return
	}
	issued, err := h.service.Login(r.Context(), input)
	if errors.Is(err, ErrUnauthenticated) {
		noStore(w)
		writeAuthError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.setSessionCookies(w, r, issued)
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"user": issued.User, "expires_at": issued.ExpiresAt})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, principal.User)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookieName)
	if cookie != nil {
		if err := h.service.Logout(r.Context(), cookie.Value); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}
	h.clearSessionCookies(w, r)
	noStore(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setSessionCookies(w http.ResponseWriter, r *http.Request, issued IssuedSession) {
	secure := r.TLS != nil || h.forceSecureCookie
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: issued.Token, Path: "/", Expires: issued.ExpiresAt,
		MaxAge: int(SessionLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: issued.CSRFToken, Path: "/", Expires: issued.ExpiresAt,
		MaxAge: int(SessionLifetime.Seconds()), Secure: secure, SameSite: http.SameSiteStrictMode})
}

func (h *Handler) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || h.forceSecureCookie
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
			HttpOnly: name == sessionCookieName, Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("body must contain one JSON object")
	}
	return nil
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

func writeAuthError(w http.ResponseWriter, status int, message string) {
	noStore(w)
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
