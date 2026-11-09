package living

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/hanadoggy/life-os/backend/internal/modules/ledger"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if os.Getenv("CI") != "" {
			t.Fatalf("cannot ping test db in CI: %v", err)
		}
		t.Skipf("cannot ping test db (%v); skipping integration test", err)
	}

	_, err = pool.Exec(ctx, `
		TRUNCATE TABLE
			monthly_budgets,
			recurring_executions,
			recurring_rules,
			furusato_donations,
			furusato_profiles,
			sheet_sync_outbox,
			settlements,
			transactions,
			expense_presets
		RESTART IDENTITY CASCADE;
	`)
	if err != nil {
		pool.Close()
		t.Fatalf("truncate tables: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Tokyo")
	wakeCh := make(chan struct{}, 10)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	livingSvc := NewService(pool, loc, wakeCh)
	livingHandler := NewHandler(livingSvc, logger, loc)

	ledgerSvc := ledger.NewService(pool, loc, wakeCh)
	ledgerHandler := ledger.NewHandler(ledgerSvc, logger, loc)

	mux := http.NewServeMux()
	livingHandler.RegisterRoutes(mux)
	ledgerHandler.RegisterRoutes(mux)

	ts := httptest.NewServer(httpx.Chain(mux, httpx.RequestID, httpx.Recover(logger)))
	t.Cleanup(func() {
		ts.Close()
		pool.Close()
	})

	return ts, pool
}

func doJSONRequest(t *testing.T, client *http.Client, method, url string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp, respBytes
}

func TestBudgetsAndLedgerSummaryIntegration(t *testing.T) {
	ts, _ := setupTestServer(t)
	client := ts.Client()

	// 1. Initially no budget is configured -> effective_budget & summary.variable_budget are null
	resp, body := doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/budgets?year=2026&month=11", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /living/budgets status = %d, body = %s", resp.StatusCode, string(body))
	}
	var overview BudgetOverviewResponse
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if overview.DefaultBudget != nil || overview.MonthlyOverride != nil || overview.EffectiveBudget != nil {
		t.Fatalf("expected all nil budgets initially, got %+v", overview)
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/ledger/summary?year=2026&month=11", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ledger/summary status = %d", resp.StatusCode)
	}
	var summary ledger.MonthlySummaryResponse
	_ = json.Unmarshal(body, &summary)
	if summary.VariableBudget != nil || summary.VariableRemaining != nil || summary.BudgetBurnRate != nil {
		t.Fatalf("expected nil budget fields in summary when no budget is configured, got %+v", summary)
	}

	// 2. Set default budget (year=0, month=0) = 80,000 JPY
	resp, body = doJSONRequest(t, client, http.MethodPut, ts.URL+"/api/v1/living/budgets", map[string]any{
		"year":            0,
		"month":           0,
		"variable_budget": 80000,
		"memo":            "基本の月間変動費予算",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT default budget status = %d, body = %s", resp.StatusCode, string(body))
	}
	var savedDefault BudgetItemResponse
	_ = json.Unmarshal(body, &savedDefault)
	if savedDefault.Year != 0 || savedDefault.Month != 0 || savedDefault.VariableBudget != 80000 {
		t.Fatalf("unexpected saved default budget: %+v", savedDefault)
	}

	// 3. Add a variable expense of 20,000 JPY in 2026-11
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/categories", nil)
	var catWrap struct {
		Items []ledger.CategoryResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &catWrap)
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/payment-methods", nil)
	var pmWrap struct {
		Items []ledger.PaymentMethodResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &pmWrap)

	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", map[string]any{
		"tx_date":           "2026-11-05",
		"kind":              "EXPENSE",
		"expense_type":      "VARIABLE",
		"category_id":       catWrap.Items[0].ID,
		"title":             "スーパー買い出し",
		"gross_amount":      20000,
		"points_used":       0,
		"payment_method_id": pmWrap.Items[0].ID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /transactions status = %d, body = %s", resp.StatusCode, string(body))
	}

	// 4. Verify summary falls back to default budget (80,000), remaining = 60,000, burn_rate = 25.0%
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/ledger/summary?year=2026&month=11", nil)
	_ = json.Unmarshal(body, &summary)
	if summary.VariableBudget == nil || *summary.VariableBudget != 80000 {
		t.Fatalf("expected VariableBudget = 80000, got %v", summary.VariableBudget)
	}
	if summary.VariableRemaining == nil || *summary.VariableRemaining != 60000 {
		t.Fatalf("expected VariableRemaining = 60000, got %v", summary.VariableRemaining)
	}
	if summary.BudgetBurnRate == nil || *summary.BudgetBurnRate != 25.0 {
		t.Fatalf("expected BudgetBurnRate = 25.0, got %v", summary.BudgetBurnRate)
	}

	// 5. Override 2026-11 budget to 50,000 JPY -> remaining = 30,000, burn_rate = 40.0%
	resp, body = doJSONRequest(t, client, http.MethodPut, ts.URL+"/api/v1/living/budgets", map[string]any{
		"year":            2026,
		"month":           11,
		"variable_budget": 50000,
		"memo":            "11月特別予算",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT monthly override status = %d, body = %s", resp.StatusCode, string(body))
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/budgets?year=2026&month=11", nil)
	_ = json.Unmarshal(body, &overview)
	if overview.EffectiveBudget == nil || *overview.EffectiveBudget != 50000 {
		t.Fatalf("expected EffectiveBudget = 50000, got %v", overview.EffectiveBudget)
	}
	if overview.DefaultBudget == nil || overview.DefaultBudget.VariableBudget != 80000 {
		t.Fatalf("expected DefaultBudget = 80000 alongside override")
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/ledger/summary?year=2026&month=11", nil)
	_ = json.Unmarshal(body, &summary)
	if summary.VariableBudget == nil || *summary.VariableBudget != 50000 {
		t.Fatalf("expected VariableBudget = 50000, got %v", summary.VariableBudget)
	}
	if summary.VariableRemaining == nil || *summary.VariableRemaining != 30000 {
		t.Fatalf("expected VariableRemaining = 30000, got %v", summary.VariableRemaining)
	}
	if summary.BudgetBurnRate == nil || *summary.BudgetBurnRate != 40.0 {
		t.Fatalf("expected BudgetBurnRate = 40.0, got %v", summary.BudgetBurnRate)
	}

	// 6. Delete 2026-11 override -> falls back to default budget (80,000)
	resp, _ = doJSONRequest(t, client, http.MethodDelete, ts.URL+"/api/v1/living/budgets?year=2026&month=11", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /living/budgets status = %d, want 204", resp.StatusCode)
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/budgets?year=2026&month=11", nil)
	_ = json.Unmarshal(body, &overview)
	if overview.MonthlyOverride != nil {
		t.Fatalf("expected MonthlyOverride to be nil after delete")
	}
	if overview.EffectiveBudget == nil || *overview.EffectiveBudget != 80000 {
		t.Fatalf("expected EffectiveBudget to fall back to 80000, got %v", overview.EffectiveBudget)
	}
}

