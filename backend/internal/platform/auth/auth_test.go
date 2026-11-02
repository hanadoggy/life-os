package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

func TestHashTokenDeterministicAndDistinct(t *testing.T) {
	h1 := HashToken("token-alpha")
	h2 := HashToken("token-alpha")
	h3 := HashToken("token-beta")

	if len(h1) != 32 {
		t.Fatalf("expected 32-byte SHA-256 hash, got %d", len(h1))
	}
	if string(h1) != string(h2) {
		t.Fatal("expected identical hashes for identical input")
	}
	if string(h1) == string(h3) {
		t.Fatal("expected distinct hashes for distinct inputs")
	}
}

func TestRequireCSRF(t *testing.T) {
	mw := RequireCSRF("https://hanayor.com")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
	}{
		{"GET without Origin is allowed", http.MethodGet, "", http.StatusOK},
		{"POST without Origin is rejected", http.MethodPost, "", http.StatusForbidden},
		{"POST with wrong Origin is rejected", http.MethodPost, "https://evil.example.com", http.StatusForbidden},
		{"POST with matching Origin is allowed", http.MethodPost, "https://hanayor.com", http.StatusOK},
		{"PATCH with matching Origin and trailing slash is allowed", http.MethodPatch, "https://hanayor.com/", http.StatusOK},
		{"DELETE with wrong Origin is rejected", http.MethodDelete, "http://hanayor.com", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/transactions", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestOAuthAndSessionLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL auth integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("cannot ping test db (%v); skipping", err)
	}
	_, _ = pool.Exec(ctx, `TRUNCATE TABLE sessions;`)

	// Mock Google OAuth token & userinfo server
	var mockEmail = "hanayor6346@gmail.com"
	var mockVerified = true
	mockGoogle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.FormValue("code") == "" || r.FormValue("code_verifier") == "" {
				http.Error(w, "missing code or code_verifier", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "mock-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sub":            "1234567890",
				"email":          mockEmail,
				"email_verified": mockVerified,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockGoogle.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	queries := db.New(pool)
	sessionMgr := NewSessionManager(queries, logger, true)

	authHandler := NewHandler(OAuthConfig{
		ClientID:         "test-client-id",
		ClientSecret:     "test-client-secret",
		RedirectURL:      "https://hanayor.com/auth/google/callback",
		AllowedUserEmail: "hanayor6346@gmail.com",
		AppBaseURL:       "https://hanayor.com",
		SecureCookie:     true,
		Endpoint: oauth2.Endpoint{
			AuthURL:  mockGoogle.URL + "/auth",
			TokenURL: mockGoogle.URL + "/token",
		},
		UserInfoURL: mockGoogle.URL + "/userinfo",
	}, sessionMgr, logger)

	publicMux := http.NewServeMux()
	authHandler.RegisterPublicRoutes(publicMux)

	protectedMux := http.NewServeMux()
	authHandler.RegisterProtectedRoutes(protectedMux)
	publicMux.Handle("/api/v1/", httpx.Chain(
		protectedMux,
		RequireAuth(sessionMgr, logger),
		RequireCSRF("https://hanayor.com"),
	))

	srv := httptest.NewServer(publicMux)
	defer srv.Close()

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Unauthenticated GET /api/v1/auth/me -> 401
	resp, err := noRedirectClient.Get(srv.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /me status = %d, want 401", resp.StatusCode)
	}

	// 2. Initiate GET /auth/google/login -> 302 with PKCE challenge & state cookie
	resp, err = noRedirectClient.Get(srv.URL + "/auth/google/login")
	if err != nil {
		t.Fatalf("GET /auth/google/login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("/login status = %d, want 302", resp.StatusCode)
	}

	var stateCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == OAuthStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("expected lifeos_oauth_state cookie")
	}

	locURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect Location: %v", err)
	}
	stateParam := locURL.Query().Get("state")
	if stateParam == "" || locURL.Query().Get("code_challenge") == "" || locURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("expected state and S256 PKCE challenge in redirect URL: %s", locURL.String())
	}

	// 3. Callback with valid state & code -> 302 to https://hanayor.com/ and issues lifeos_session cookie
	cbReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/auth/google/callback?state="+url.QueryEscape(stateParam)+"&code=valid-auth-code", nil)
	cbReq.AddCookie(stateCookie)
	cbResp, err := noRedirectClient.Do(cbReq)
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	cbResp.Body.Close()
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("/callback status = %d, want 302", cbResp.StatusCode)
	}

	var sessionCookie *http.Cookie
	for _, c := range cbResp.Cookies() {
		if c.Name == SessionCookieName && c.Value != "" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected lifeos_session cookie after successful callback")
	}
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("unexpected cookie security flags: HttpOnly=%v Secure=%v SameSite=%v",
			sessionCookie.HttpOnly, sessionCookie.Secure, sessionCookie.SameSite)
	}

	// 4. Authenticated GET /api/v1/auth/me -> 200 OK with email
	meReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/me", nil)
	meReq.AddCookie(sessionCookie)
	meResp, err := noRedirectClient.Do(meReq)
	if err != nil {
		t.Fatalf("authenticated GET /me: %v", err)
	}
	body, _ := io.ReadAll(meResp.Body)
	meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK || !strings.Contains(string(body), "hanayor6346@gmail.com") {
		t.Fatalf("unexpected /me response: status=%d body=%s", meResp.StatusCode, string(body))
	}

	// 5. POST /api/v1/auth/logout without Origin -> 403 CSRF
	logoutNoOrigin, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	logoutNoOrigin.AddCookie(sessionCookie)
	logoutResp1, _ := noRedirectClient.Do(logoutNoOrigin)
	logoutResp1.Body.Close()
	if logoutResp1.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without Origin status = %d, want 403", logoutResp1.StatusCode)
	}

	// 6. POST /api/v1/auth/logout with matching Origin -> 204 No Content and invalidates session in DB
	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutReq.Header.Set("Origin", "https://hanayor.com")
	logoutResp2, _ := noRedirectClient.Do(logoutReq)
	logoutResp2.Body.Close()
	if logoutResp2.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp2.StatusCode)
	}

	// 7. Re-using the logged-out session cookie -> 401 Unauthorized
	meAfterLogout, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/me", nil)
	meAfterLogout.AddCookie(sessionCookie)
	meAfterResp, _ := noRedirectClient.Do(meAfterLogout)
	meAfterResp.Body.Close()
	if meAfterResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/me after logout status = %d, want 401", meAfterResp.StatusCode)
	}

	// 8. Unauthorized email attempt -> 403 Forbidden
	mockEmail = "intruder@example.com"
	resp2, _ := noRedirectClient.Get(srv.URL + "/auth/google/login")
	resp2.Body.Close()
	var stateCookie2 *http.Cookie
	for _, c := range resp2.Cookies() {
		if c.Name == OAuthStateCookieName {
			stateCookie2 = c
		}
	}
	locURL2, _ := url.Parse(resp2.Header.Get("Location"))
	cbReq2, _ := http.NewRequest(http.MethodGet, srv.URL+"/auth/google/callback?state="+url.QueryEscape(locURL2.Query().Get("state"))+"&code=valid-code", nil)
	cbReq2.AddCookie(stateCookie2)
	cbResp2, _ := noRedirectClient.Do(cbReq2)
	cbResp2.Body.Close()
	if cbResp2.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized email callback status = %d, want 403", cbResp2.StatusCode)
	}
}

