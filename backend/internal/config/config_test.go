package config

import (
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://lifeos:pass@localhost:5432/lifeos_dev?sslmode=disable")
	t.Setenv("APP_BASE_URL", "https://hanayor.com/")
	t.Setenv("PORT", "8080")
	t.Setenv("TZ", "Asia/Tokyo")
	t.Setenv("SHEETS_SPREADSHEET_IDS", "2025=sheet2025, 2026=sheet2026")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.AppBaseURL != "https://hanayor.com" {
		t.Errorf("AppBaseURL = %q, want %q", cfg.AppBaseURL, "https://hanayor.com")
	}
	if cfg.SheetsSpreadsheetIDs[2025] != "sheet2025" || cfg.SheetsSpreadsheetIDs[2026] != "sheet2026" {
		t.Errorf("unexpected SheetsSpreadsheetIDs: %+v", cfg.SheetsSpreadsheetIDs)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "missing DATABASE_URL",
			env: map[string]string{
				"DATABASE_URL": "",
				"APP_BASE_URL": "https://hanayor.com",
			},
		},
		{
			name: "invalid APP_BASE_URL",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/db",
				"APP_BASE_URL": "not-a-url",
			},
		},
		{
			name: "invalid SHEETS_SPREADSHEET_IDS format",
			env: map[string]string{
				"DATABASE_URL":           "postgres://localhost/db",
				"APP_BASE_URL":           "https://hanayor.com",
				"SHEETS_SPREADSHEET_IDS": "invalid_entry",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

