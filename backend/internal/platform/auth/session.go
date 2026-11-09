package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	// SessionCookieName is the HttpOnly session cookie name (Specification §3.1).
	SessionCookieName = "lifeos_session"
	// OAuthStateCookieName stores the short-lived OAuth state + PKCE verifier.
	OAuthStateCookieName = "lifeos_oauth_state"
	// SessionTTL is the 90-day sliding session expiration for iOS PWA convenience.
	SessionTTL = 90 * 24 * time.Hour
	// SessionTouchInterval throttles DB writes when sliding session expiration.
	SessionTouchInterval = 1 * time.Hour
	// OAuthStateTTL is the 10-minute lifetime for the OAuth login handshake cookie.
	OAuthStateTTL = 10 * time.Minute
)

type ctxKey int

const userEmailKey ctxKey = iota

// ErrUnauthenticated is returned when a request lacks a valid active session.
var ErrUnauthenticated = errors.New("unauthenticated")

// UserEmailFromContext extracts the authenticated user's email from ctx.
func UserEmailFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userEmailKey).(string)
	return v, ok && v != ""
}

// ContextWithUserEmail returns a new context carrying the authenticated user's email.
func ContextWithUserEmail(ctx context.Context, email string) context.Context {
	return context.WithValue(ctx, userEmailKey, email)
}

// HashToken returns the SHA-256 digest of a raw session token.
// Only the hash is stored in PostgreSQL so a DB leak cannot compromise active sessions.
func HashToken(rawToken string) []byte {
	sum := sha256.Sum256([]byte(rawToken))
	return sum[:]
}

// GenerateRandomToken generates a URL-safe base64 token from numBytes of crypto/rand entropy.
func GenerateRandomToken(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SessionManager manages server-side sessions backed by PostgreSQL.
type SessionManager struct {
	queries      *db.Queries
	logger       *slog.Logger
	secureCookie bool
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(queries *db.Queries, logger *slog.Logger, secureCookie bool) *SessionManager {
	return &SessionManager{
		queries:      queries,
		logger:       logger,
		secureCookie: secureCookie,
	}
}

// CreateSession generates a 32-byte random token, stores its SHA-256 hash in the DB, and sets the session cookie.
func (m *SessionManager) CreateSession(ctx context.Context, w http.ResponseWriter, email string) error {
	rawToken, err := GenerateRandomToken(32)
	if err != nil {
		return err
	}
	tokenHash := HashToken(rawToken)
	expiresAt := time.Now().UTC().Add(SessionTTL)

	if _, err := m.queries.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: tokenHash,
		Email:     email,
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("create session row: %w", err)
	}

	m.setSessionCookie(w, rawToken, expiresAt)
	return nil
}

// ValidateAndSlideSession verifies the session cookie, extends its 90-day sliding window, and returns the user's email.
func (m *SessionManager) ValidateAndSlideSession(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", ErrUnauthenticated
	}

	rawToken := strings.TrimSpace(cookie.Value)
	tokenHash := HashToken(rawToken)

	sess, err := m.queries.GetActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrUnauthenticated
		}
		return "", fmt.Errorf("get active session: %w", err)
	}

	if time.Since(sess.LastSeenAt) >= SessionTouchInterval {
		newExpiresAt := time.Now().UTC().Add(SessionTTL)
		if err := m.queries.TouchSession(ctx, db.TouchSessionParams{
			TokenHash: tokenHash,
			ExpiresAt: newExpiresAt,
		}); err != nil {
			m.logger.WarnContext(ctx, "failed_to_slide_session", slog.String("error", err.Error()))
		} else {
			m.setSessionCookie(w, rawToken, newExpiresAt)
		}
	}

	return sess.Email, nil
}

// DestroySession deletes the session from PostgreSQL and expires the browser cookie.
func (m *SessionManager) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		tokenHash := HashToken(strings.TrimSpace(cookie.Value))
		if err := m.queries.DeleteSessionByTokenHash(ctx, tokenHash); err != nil {
			m.logger.WarnContext(ctx, "failed_to_delete_session_row", slog.String("error", err.Error()))
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
	return nil
}

// RunCleanupWorker periodically removes expired sessions from PostgreSQL (Specification §7.3).
func (m *SessionManager) RunCleanupWorker(ctx context.Context) {
	m.sweepExpired(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepExpired(ctx)
		}
	}
}

func (m *SessionManager) sweepExpired(ctx context.Context) {
	cleanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	deleted, err := m.queries.DeleteExpiredSessions(cleanCtx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			m.logger.ErrorContext(ctx, "session_cleanup_failed", slog.String("error", err.Error()))
		}
		return
	}
	if deleted > 0 {
		m.logger.InfoContext(ctx, "expired_sessions_cleaned", slog.Int64("deleted", deleted))
	}
}

func (m *SessionManager) setSessionCookie(w http.ResponseWriter, rawToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL.Seconds()),
		Expires:  expiresAt,
	})
}

