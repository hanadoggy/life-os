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

func TestRecurringRulesAndSchedulerIdempotencyIntegration(t *testing.T) {
	ts, pool := setupTestServer(t)
	client := ts.Client()
	ctx := context.Background()

	// Fetch reference categories and payment methods
	resp, body := doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/categories", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /categories status = %d", resp.StatusCode)
	}
	var catWrap struct {
		Items []ledger.CategoryResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &catWrap)

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/payment-methods", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /payment-methods status = %d", resp.StatusCode)
	}
	var pmWrap struct {
		Items []ledger.PaymentMethodResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &pmWrap)

	var rentCatID, subCatID int32
	for _, c := range catWrap.Items {
		if c.Name == "RENT" {
			rentCatID = c.ID
		} else if c.Name == "SUBSCRIPTION" {
			subCatID = c.ID
		}
	}
	if rentCatID == 0 {
		rentCatID = catWrap.Items[0].ID
	}
	if subCatID == 0 {
		subCatID = catWrap.Items[0].ID
	}

	var creditCardPMID, advancePMID int32
	for _, pm := range pmWrap.Items {
		if pm.Name == "CREDIT_CARD" {
			creditCardPMID = pm.ID
		} else if pm.Name == "ADVANCE" {
			advancePMID = pm.ID
		}
	}

	// 1. Reject ADVANCE payment method on recurring rule creation (422)
	resp, _ = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules", map[string]any{
		"title":             "不正な立替ルール",
		"expense_type":      "FIXED",
		"category_id":       rentCatID,
		"amount":            50000,
		"payment_method_id": advancePMID,
		"frequency":         "MONTHLY",
		"day_of_month":      1,
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for ADVANCE payment method in recurring rule, got %d", resp.StatusCode)
	}

	// 2. Create Rule A: Monthly rent on day 31 (tests end-of-month clamping: e.g. 2026-02 -> 2026-02-28)
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules", map[string]any{
		"title":             "家賃・管理費",
		"expense_type":      "FIXED",
		"category_id":       rentCatID,
		"amount":            98000,
		"payment_method_id": creditCardPMID,
		"frequency":         "MONTHLY",
		"day_of_month":      31,
		"auto_create":       true,
		"is_subscription":   false,
		"memo":              "月末引き落とし",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /recurring-rules (rent) status = %d, body = %s", resp.StatusCode, string(body))
	}
	var rentRule RecurringRuleResponse
	_ = json.Unmarshal(body, &rentRule)

	// 3. Create Rule B: Yearly subscription in February (month_of_year = 2, day_of_month = 15)
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules", map[string]any{
		"title":             "Amazon Prime 年会費",
		"expense_type":      "FIXED",
		"category_id":       subCatID,
		"amount":            5900,
		"payment_method_id": creditCardPMID,
		"frequency":         "YEARLY",
		"day_of_month":      15,
		"month_of_year":     2,
		"auto_create":       true,
		"is_subscription":   true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /recurring-rules (prime) status = %d, body = %s", resp.StatusCode, string(body))
	}
	var primeRule RecurringRuleResponse
	_ = json.Unmarshal(body, &primeRule)

	// 4. Create Rule C: Manual subscription (auto_create = false) -> should NOT be auto-created by sync
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules", map[string]any{
		"title":             "Spotify",
		"expense_type":      "FIXED",
		"category_id":       subCatID,
		"amount":            980,
		"payment_method_id": creditCardPMID,
		"frequency":         "MONTHLY",
		"day_of_month":      5,
		"auto_create":       false,
		"is_subscription":   true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /recurring-rules (spotify) status = %d, body = %s", resp.StatusCode, string(body))
	}
	var spotifyRule RecurringRuleResponse
	_ = json.Unmarshal(body, &spotifyRule)

	// 5. Sync 2026-02 (February 2026): both Rule A (clamped to 2026-02-28) and Rule B (2026-02-15) should execute
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules/sync?year=2026&month=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /recurring-rules/sync status = %d, body = %s", resp.StatusCode, string(body))
	}
	var syncResp SyncRecurringRulesResponse
	_ = json.Unmarshal(body, &syncResp)
	if syncResp.CreatedCount != 2 || len(syncResp.TransactionIDs) != 2 {
		t.Fatalf("expected CreatedCount = 2 for 2026-02, got %+v", syncResp)
	}

	// 6. Re-run Sync for 2026-02 -> idempotency check (UNIQUE(rule_id, target_period)): CreatedCount must be 0
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/recurring-rules/sync", map[string]any{
		"year":  2026,
		"month": 2,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second POST /recurring-rules/sync status = %d, body = %s", resp.StatusCode, string(body))
	}
	_ = json.Unmarshal(body, &syncResp)
	if syncResp.CreatedCount != 0 {
		t.Fatalf("expected CreatedCount = 0 on duplicate sync, got %+v", syncResp)
	}

	// 7. Verify 2026-02 transactions in Ledger and sheet_sync_outbox entries
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions?year=2026&month=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /transactions?year=2026&month=2 status = %d", resp.StatusCode)
	}
	var txList struct {
		Items []ledger.TransactionResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &txList)
	if len(txList.Items) != 2 {
		t.Fatalf("expected 2 transactions in 2026-02, got %d", len(txList.Items))
	}
	// First item ordered by tx_date DESC should be clamped rent on 2026-02-28
	if txList.Items[0].TxDate != "2026-02-28" || txList.Items[0].GrossAmount != 98000 {
		t.Fatalf("expected clamped rent tx on 2026-02-28 with 98000, got %+v", txList.Items[0])
	}
	if txList.Items[1].TxDate != "2026-02-15" || txList.Items[1].GrossAmount != 5900 {
		t.Fatalf("expected prime yearly tx on 2026-02-15 with 5900, got %+v", txList.Items[1])
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM sheet_sync_outbox WHERE op = 'UPSERT'").Scan(&outboxCount); err != nil {
		t.Fatalf("count sheet_sync_outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Fatalf("expected 2 sheet_sync_outbox UPSERT rows, got %d", outboxCount)
	}

	// 8. Verify GET /api/v1/living/recurring-rules?year=2026&month=2 marks executed_in_month = true for Rule A & B
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/recurring-rules?year=2026&month=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /recurring-rules status = %d", resp.StatusCode)
	}
	var rulesOverview RecurringRulesOverviewResponse
	_ = json.Unmarshal(body, &rulesOverview)
	if len(rulesOverview.Items) != 3 {
		t.Fatalf("expected 3 rules in overview, got %d", len(rulesOverview.Items))
	}
	for _, item := range rulesOverview.Items {
		switch item.ID {
		case rentRule.ID, primeRule.ID:
			if !item.ExecutedInMonth || item.TransactionID == nil {
				t.Fatalf("expected rule %s to have executed_in_month=true in 2026-02, got %+v", item.Title, item)
			}
		case spotifyRule.ID:
			if item.ExecutedInMonth {
				t.Fatalf("expected manual rule Spotify to have executed_in_month=false")
			}
		}
	}

	// 9. Update Rule C (Spotify) amount and delete it
	resp, body = doJSONRequest(t, client, http.MethodPatch, ts.URL+"/api/v1/living/recurring-rules/"+spotifyRule.ID, map[string]any{
		"amount":      1080,
		"auto_create": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /recurring-rules/%s status = %d, body = %s", spotifyRule.ID, resp.StatusCode, string(body))
	}
	var updatedSpotify RecurringRuleResponse
	_ = json.Unmarshal(body, &updatedSpotify)
	if updatedSpotify.Amount != 1080 || !updatedSpotify.AutoCreate {
		t.Fatalf("unexpected updated rule: %+v", updatedSpotify)
	}

	resp, _ = doJSONRequest(t, client, http.MethodDelete, ts.URL+"/api/v1/living/recurring-rules/"+spotifyRule.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /recurring-rules/%s status = %d, want 204", spotifyRule.ID, resp.StatusCode)
	}
}

func TestFurusatoProfileDonationsAndExpenseSyncIntegration(t *testing.T) {
	ts, pool := setupTestServer(t)
	client := ts.Client()
	ctx := context.Background()

	// 1. Initial GET /api/v1/living/furusato?year=2026 returns default profile and formula metadata
	resp, body := doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/furusato?year=2026", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /living/furusato status = %d, body = %s", resp.StatusCode, string(body))
	}
	var overview FurusatoOverviewResponse
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("unmarshal furusato overview: %v", err)
	}
	if overview.TaxYear != 2026 || overview.EstimatedLimit != 0 || overview.FormulaUpdatedAt != "2026-04" {
		t.Fatalf("unexpected initial furusato overview: %+v", overview)
	}
	if overview.FormulaLabel != FurusatoFormulaLabel {
		t.Fatalf("expected FormulaLabel = %q, got %q", FurusatoFormulaLabel, overview.FormulaLabel)
	}

	// 2. PUT /api/v1/living/furusato/profile/2026 with gross_income = 6,000,000 JPY (auto 15% social insurance)
	resp, body = doJSONRequest(t, client, http.MethodPut, ts.URL+"/api/v1/living/furusato/profile/2026", map[string]any{
		"gross_income":     6000000,
		"social_insurance": 0,
		"ideco_annual":     0,
		"family_status":    "SINGLE",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /living/furusato/profile/2026 status = %d, body = %s", resp.StatusCode, string(body))
	}
	_ = json.Unmarshal(body, &overview)
	if overview.EstimatedLimit != 77900 || overview.RemainingLimit != 77900 {
		t.Fatalf("expected EstimatedLimit = 77900, got %+v", overview)
	}

	// 3. POST /api/v1/living/furusato/donations with create_expense = true
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/furusato/donations", map[string]any{
		"tax_year":        2026,
		"donated_at":      "2026-11-10",
		"municipality":    "北海道白糠町",
		"return_gift":     "いくら醤油漬け 500g",
		"amount":          15000,
		"onestop_method":  "ONLINE_IAM",
		"create_expense":  true,
		"memo":            "楽天ふるさと納税",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /living/furusato/donations status = %d, body = %s", resp.StatusCode, string(body))
	}
	var donation1 FurusatoDonationResponse
	_ = json.Unmarshal(body, &donation1)
	if donation1.TransactionID == nil || *donation1.TransactionID == "" {
		t.Fatalf("expected linked transaction_id when create_expense=true, got %+v", donation1)
	}
	if donation1.ApplicationDeadline != "2027-01-10" || donation1.OnestopCompleted {
		t.Fatalf("expected default deadline 2027-01-10 and onestop_completed=false, got %+v", donation1)
	}

	// Verify linked PUBLIC_FEES transaction in Ledger and sheet_sync_outbox entry
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions?year=2026&month=11", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /transactions status = %d", resp.StatusCode)
	}
	var txList struct {
		Items []ledger.TransactionResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &txList)
	if len(txList.Items) != 1 || txList.Items[0].GrossAmount != 15000 {
		t.Fatalf("expected 1 linked PUBLIC_FEES transaction of 15000 JPY, got %+v", txList.Items)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM sheet_sync_outbox WHERE op = 'UPSERT'").Scan(&outboxCount); err != nil {
		t.Fatalf("count sheet_sync_outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 sheet_sync_outbox UPSERT row for furusato expense, got %d", outboxCount)
	}

	// 4. POST second donation without create_expense (POSTAL method)
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/living/furusato/donations", map[string]any{
		"tax_year":       2026,
		"donated_at":     "2026-11-12",
		"municipality":   "宮崎県都城市",
		"return_gift":    "宮崎牛切り落とし",
		"amount":         12000,
		"onestop_method": "POSTAL",
		"create_expense": false,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST second donation status = %d, body = %s", resp.StatusCode, string(body))
	}
	var donation2 FurusatoDonationResponse
	_ = json.Unmarshal(body, &donation2)
	if donation2.TransactionID != nil {
		t.Fatalf("expected nil transaction_id when create_expense=false, got %+v", donation2)
	}

	// 5. Verify overview totals: donated_total = 27,000, remaining_limit = 50,900, uncompleted_onestop_count = 2
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/furusato?year=2026", nil)
	_ = json.Unmarshal(body, &overview)
	if overview.DonatedTotal != 27000 || overview.RemainingLimit != 50900 || overview.UncompletedOnestopCount != 2 {
		t.Fatalf("unexpected overview totals after 2 donations: %+v", overview)
	}

	// 6. Toggle onestop_completed = true on donation1 via PATCH
	resp, body = doJSONRequest(t, client, http.MethodPatch, ts.URL+"/api/v1/living/furusato/donations/"+donation1.ID, map[string]any{
		"onestop_completed": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH furusato donation status = %d, body = %s", resp.StatusCode, string(body))
	}
	var patched1 FurusatoDonationResponse
	_ = json.Unmarshal(body, &patched1)
	if !patched1.OnestopCompleted || patched1.CompletedAt == nil {
		t.Fatalf("expected OnestopCompleted=true and non-nil CompletedAt, got %+v", patched1)
	}

	// Verify uncompleted_onestop_count is now 1
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/furusato?year=2026", nil)
	_ = json.Unmarshal(body, &overview)
	if overview.UncompletedOnestopCount != 1 {
		t.Fatalf("expected UncompletedOnestopCount = 1, got %d", overview.UncompletedOnestopCount)
	}

	// 7. Delete donation2
	resp, _ = doJSONRequest(t, client, http.MethodDelete, ts.URL+"/api/v1/living/furusato/donations/"+donation2.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE furusato donation status = %d, want 204", resp.StatusCode)
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/living/furusato?year=2026", nil)
	_ = json.Unmarshal(body, &overview)
	if len(overview.Items) != 1 || overview.DonatedTotal != 15000 || overview.UncompletedOnestopCount != 0 {
		t.Fatalf("unexpected overview after delete: %+v", overview)
	}
}



