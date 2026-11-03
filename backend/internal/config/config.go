package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the LifeOS backend server.
type Config struct {
	Port        string
	AppEnv      string
	AppBaseURL  string
	Timezone    string
	Location    *time.Location
	DatabaseURL string

	// Auth (Google OAuth 2.0 + Server Session)
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	GoogleOAuthRedirectURL  string
	AllowedUserEmail        string

	// Google Sheets Write-behind Sync
	SheetsEnabled        bool
	SheetsSpreadsheetIDs map[int]string
	SheetsMonthTabFormat string
	SheetsTxIDColumn     string
	SheetsAppLogTab      string
}

// Load reads configuration from environment variables and validates required values.
func Load() (*Config, error) {
	tz := getEnv("TZ", "Asia/Tokyo")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid TZ %q: %w", tz, err)
	}

	cfg := &Config{
		Port:                    getEnv("PORT", "8080"),
		AppEnv:                  getEnv("APP_ENV", "development"),
		AppBaseURL:              strings.TrimRight(getEnv("APP_BASE_URL", "http://localhost:5173"), "/"),
		Timezone:                tz,
		Location:                loc,
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		GoogleOAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GoogleOAuthRedirectURL:  os.Getenv("GOOGLE_OAUTH_REDIRECT_URL"),
		AllowedUserEmail:        strings.TrimSpace(os.Getenv("ALLOWED_USER_EMAIL")),
		SheetsEnabled:           getEnvBool("SHEETS_ENABLED", false),
		SheetsMonthTabFormat:    getEnv("SHEETS_MONTH_TAB_FORMAT", "{Month}"),
		SheetsTxIDColumn:        getEnv("SHEETS_TXID_COLUMN", "Z"),
		SheetsAppLogTab:         getEnv("SHEETS_APP_LOG_TAB", "app_log"),
	}

	sheetIDs, err := parseSpreadsheetIDs(os.Getenv("SHEETS_SPREADSHEET_IDS"))
	if err != nil {
		return nil, fmt.Errorf("parse SHEETS_SPREADSHEET_IDS: %w", err)
	}
	cfg.SheetsSpreadsheetIDs = sheetIDs

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks mandatory configuration fields and format constraints.
func (c *Config) Validate() error {
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("PORT must be a valid integer: %w", err)
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	u, err := url.Parse(c.AppBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("APP_BASE_URL must be a valid http(s) origin: %q", c.AppBaseURL)
	}
	return nil
}

// IsProduction returns true when APP_ENV is set to "production".
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

func parseSpreadsheetIDs(raw string) (map[int]string, error) {
	res := make(map[int]string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return res, nil
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid entry %q (expected YYYY=spreadsheet_id)", pair)
		}
		year, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || year < 2000 || year > 2100 {
			return nil, fmt.Errorf("invalid year in %q", pair)
		}
		id := strings.TrimSpace(parts[1])
		if id == "" {
			return nil, fmt.Errorf("empty spreadsheet_id for year %d", year)
		}
		res[year] = id
	}
	return res, nil
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

