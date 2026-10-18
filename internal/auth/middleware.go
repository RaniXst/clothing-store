package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/RaniXst/clothing-store/internal/user"
)

type principalKey struct{}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

func (s *Service) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		principal, err := s.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) || user.IsNotFound(err) {
				writeAuthError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			writeAuthError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal))
		next.ServeHTTP(w, r)
	})
}

func RequireRoles(roleNames ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roleNames))
	for _, name := range roleNames {
		allowed[name] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if _, ok := allowed[principal.User.Role.Name]; !ok {
				writeAuthError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		csrfCookie, err := r.Cookie(csrfCookieName)
		header := r.Header.Get("X-CSRF-Token")
		if err != nil || csrfCookie.Value == "" || header == "" || !constantTimeEqual(csrfCookie.Value, header) || !constantTimeEqual(tokenDigestForCSRF(csrfCookie.Value), principal.CSRFHash) {
			writeAuthError(w, http.StatusForbidden, "CSRF validation failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenDigestForCSRF(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
