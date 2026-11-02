package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
)

// RequireAuth enforces a valid session cookie on protected routes.
func RequireAuth(sessionMgr *SessionManager, logger *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			email, err := sessionMgr.ValidateAndSlideSession(r.Context(), w, r)
			if err != nil {
				if !errors.Is(err, ErrUnauthenticated) {
					logger.ErrorContext(r.Context(), "session_validation_error", slog.String("error", err.Error()))
				}
				httpx.WriteError(w,
					http.StatusUnauthorized,
					"https://hanayor.com/problems/unauthorized",
					"Unauthorized",
					"Valid session cookie is required",
				)
				return
			}

			next.ServeHTTP(w, r.WithContext(ContextWithUserEmail(r.Context(), email)))
		})
	}
}

// RequireCSRF validates that state-changing requests (POST, PATCH, DELETE, PUT) carry an Origin header
// matching the configured APP_BASE_URL (Specification §3.1, §6.0).
func RequireCSRF(appBaseURL string) httpx.Middleware {
	expectedOrigin := strings.TrimRight(strings.TrimSpace(appBaseURL), "/")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut:
				origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
				if origin == "" || !strings.EqualFold(origin, expectedOrigin) {
					httpx.WriteError(w,
						http.StatusForbidden,
						"https://hanayor.com/problems/csrf-origin-mismatch",
						"Forbidden",
						"Origin header does not match application base URL",
					)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

