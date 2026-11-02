package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const defaultGoogleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// OAuthConfig holds settings for the Google OAuth 2.0 handler.
type OAuthConfig struct {
	ClientID         string
	ClientSecret     string
	RedirectURL      string
	AllowedUserEmail string
	AppBaseURL       string
	SecureCookie     bool
	// Optional overrides for testing
	Endpoint    oauth2.Endpoint
	UserInfoURL string
}

// Handler handles /auth/google/* and /api/v1/auth/* endpoints.
type Handler struct {
	oauthCfg         *oauth2.Config
	allowedUserEmail string
	appBaseURL       string
	secureCookie     bool
	userInfoURL      string
	sessionMgr       *SessionManager
	logger           *slog.Logger
}

// NewHandler creates a new Google OAuth + Session HTTP handler.
func NewHandler(cfg OAuthConfig, sessionMgr *SessionManager, logger *slog.Logger) *Handler {
	endpoint := cfg.Endpoint
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		endpoint = google.Endpoint
	}
	userInfoURL := cfg.UserInfoURL
	if userInfoURL == "" {
		userInfoURL = defaultGoogleUserInfoURL
	}

	return &Handler{
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     endpoint,
		},
		allowedUserEmail: strings.TrimSpace(cfg.AllowedUserEmail),
		appBaseURL:       strings.TrimRight(strings.TrimSpace(cfg.AppBaseURL), "/"),
		secureCookie:     cfg.SecureCookie,
		userInfoURL:      userInfoURL,
		sessionMgr:       sessionMgr,
		logger:           logger,
	}
}

// RegisterPublicRoutes registers unauthenticated OAuth redirect routes on mux.
func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/google/login", h.handleGoogleLogin)
	mux.HandleFunc("GET /auth/google/callback", h.handleGoogleCallback)
}

// RegisterProtectedRoutes registers session-protected auth routes (/api/v1/auth/*) on mux.
func (h *Handler) RegisterProtectedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/me", h.handleMe)
	mux.HandleFunc("POST /api/v1/auth/logout", h.handleLogout)
}

func (h *Handler) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauthCfg.ClientID == "" || h.oauthCfg.ClientSecret == "" {
		httpx.WriteError(w,
			http.StatusServiceUnavailable,
			"https://hanayor.com/problems/oauth-not-configured",
			"OAuth Not Configured",
			"Google OAuth client credentials are not configured",
		)
		return
	}

	state, err := GenerateRandomToken(32)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "oauth_state_gen_failed", slog.String("error", err.Error()))
		httpx.WriteError(w, http.StatusInternalServerError, "https://hanayor.com/problems/internal-error", "Internal Server Error", "Failed to initiate login")
		return
	}
	verifier := oauth2.GenerateVerifier()

	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    state + "|" + verifier,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(OAuthStateTTL.Seconds()),
	})

	authURL := h.oauthCfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func (h *Handler) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(OAuthStateCookieName)
	// Always clear the one-time state cookie immediately
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})

	if err != nil || stateCookie.Value == "" {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/invalid-oauth-state", "Bad Request", "Missing or expired OAuth state cookie")
		return
	}

	parts := strings.SplitN(stateCookie.Value, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/invalid-oauth-state", "Bad Request", "Malformed OAuth state cookie")
		return
	}
	expectedState, verifier := parts[0], parts[1]

	queryState := r.URL.Query().Get("state")
	if subtle.ConstantTimeCompare([]byte(queryState), []byte(expectedState)) != 1 {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/invalid-oauth-state", "Bad Request", "OAuth state mismatch")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/missing-oauth-code", "Bad Request", "Missing authorization code")
		return
	}

	exchCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	token, err := h.oauthCfg.Exchange(exchCtx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		h.logger.WarnContext(r.Context(), "oauth_code_exchange_failed", slog.String("error", err.Error()))
		httpx.WriteError(w, http.StatusBadGateway, "https://hanayor.com/problems/oauth-exchange-failed", "Bad Gateway", "Failed to exchange authorization code")
		return
	}

	userInfo, err := h.fetchUserInfo(exchCtx, token)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "oauth_userinfo_failed", slog.String("error", err.Error()))
		httpx.WriteError(w, http.StatusBadGateway, "https://hanayor.com/problems/oauth-userinfo-failed", "Bad Gateway", "Failed to retrieve Google user info")
		return
	}

	if !userInfo.EmailVerified {
		httpx.WriteError(w, http.StatusForbidden, "https://hanayor.com/problems/email-not-verified", "Forbidden", "Google account email is not verified")
		return
	}

	if h.allowedUserEmail == "" || !strings.EqualFold(strings.TrimSpace(userInfo.Email), h.allowedUserEmail) {
		h.logger.WarnContext(r.Context(), "unauthorized_login_attempt", slog.String("email", userInfo.Email))
		httpx.WriteError(w, http.StatusForbidden, "https://hanayor.com/problems/forbidden-user", "Forbidden", "This Google account is not authorized to access LifeOS")
		return
	}

	if err := h.sessionMgr.CreateSession(r.Context(), w, strings.ToLower(strings.TrimSpace(userInfo.Email))); err != nil {
		h.logger.ErrorContext(r.Context(), "create_session_failed", slog.String("error", err.Error()))
		httpx.WriteError(w, http.StatusInternalServerError, "https://hanayor.com/problems/internal-error", "Internal Server Error", "Failed to create session")
		return
	}

	redirectTarget := "/"
	if h.appBaseURL != "" {
		redirectTarget = h.appBaseURL + "/"
	}
	http.Redirect(w, r, redirectTarget, http.StatusFound)
}

func (h *Handler) fetchUserInfo(ctx context.Context, token *oauth2.Token) (*googleUserInfo, error) {
	client := h.oauthCfg.Client(ctx, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo status %d", resp.StatusCode)
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	email, ok := UserEmailFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "https://hanayor.com/problems/unauthorized", "Unauthorized", "Valid session cookie is required")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"email": email,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = h.sessionMgr.DestroySession(r.Context(), w, r)
	w.WriteHeader(http.StatusNoContent)
}

