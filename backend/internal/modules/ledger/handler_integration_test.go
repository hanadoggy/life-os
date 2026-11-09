package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, <-chan struct{}) {
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

	// Clean mutable tables before test while keeping seeded categories & payment_methods.
	_, err = pool.Exec(ctx, `
		TRUNCATE TABLE sheet_sync_outbox, settlements, transactions, expense_presets RESTART IDENTITY CASCADE;
	`)
	if err != nil {
		pool.Close()
		t.Fatalf("truncate tables: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Tokyo")
	wakeCh := make(chan struct{}, 10)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	svc := NewService(pool, loc, wakeCh)
	h := NewHandler(svc, logger, loc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ts := httptest.NewServer(httpx.Chain(mux, httpx.RequestID, httpx.Recover(logger)))
	t.Cleanup(func() {
		ts.Close()
		pool.Close()
	})

	return ts, pool, wakeCh
}

func doJSONRequest(t *testing.T, client *http.Client, method, url string, headers map[string]string, body any) (*http.Response, []byte) {
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
	for k, v := range headers {
		req.Header.Set(k, v)
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

func TestLedgerEndToEndIntegration(t *testing.T) {
	ts, pool, wakeCh := setupTestServer(t)
	client := ts.Client()

	// 1. Verify seeded categories (15) and payment methods (5)
	resp, body := doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/categories", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /categories status = %d, body = %s", resp.StatusCode, string(body))
	}
	var catWrap struct {
		Items []CategoryResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &catWrap)
	if len(catWrap.Items) < 15 {
		t.Fatalf("expected at least 15 seeded categories, got %d", len(catWrap.Items))
	}

	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/payment-methods", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /payment-methods status = %d", resp.StatusCode)
	}
	var pmWrap struct {
		Items []PaymentMethodResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &pmWrap)
	if len(pmWrap.Items) < 5 {
		t.Fatalf("expected at least 5 seeded payment methods, got %d", len(pmWrap.Items))
	}

	var creditCardID, advanceID int32
	for _, pm := range pmWrap.Items {
		if pm.Name == "CREDIT_CARD" {
			creditCardID = pm.ID
		}
		if pm.Name == PaymentMethodAdvance {
			advanceID = pm.ID
		}
	}
	groceriesCatID := catWrap.Items[0].ID

	// 2. Create transaction with Idempotency-Key + points + RECEIVABLE settlements
	idemKey := "019a4567-89ab-7def-8000-000000000001"
	createPayload := map[string]any{
		"tx_date":           "2026-11-02",
		"kind":              "EXPENSE",
		"expense_type":      "VARIABLE",
		"category_id":       groceriesCatID,
		"title":             "라멘회 (멘야 무사시)",
		"gross_amount":      3600,
		"points_used":       600,
		"payment_method_id": creditCardID,
		"memo":              "에이키, 테츠와 저녁",
		"metadata":          map[string]any{"tags": []string{"ramen_club"}},
		"settlements": []map[string]any{
			{"peer_name": "에이키", "direction": "RECEIVABLE", "amount": 1200, "method": "PayPay"},
			{"peer_name": "테츠", "direction": "RECEIVABLE", "amount": 1200, "method": "PayPay"},
		},
	}

	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", map[string]string{
		"Idempotency-Key": idemKey,
	}, createPayload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /transactions status = %d, body = %s", resp.StatusCode, string(body))
	}

	var createdTx TransactionResponse
	if err := json.Unmarshal(body, &createdTx); err != nil {
		t.Fatalf("unmarshal createdTx: %v", err)
	}
	if createdTx.NetAmount != 3000 || createdTx.ReceivableTotal != 2400 || createdTx.MyShare != 600 {
		t.Fatalf("unexpected accounting calculation: net=%d receivable=%d my_share=%d",
			createdTx.NetAmount, createdTx.ReceivableTotal, createdTx.MyShare)
	}
	if len(createdTx.Settlements) != 2 {
		t.Fatalf("expected 2 settlements, got %d", len(createdTx.Settlements))
	}

	// Verify wake signal was sent and outbox row was recorded
	select {
	case <-wakeCh:
	default:
		t.Errorf("expected worker wake signal after CreateTransaction")
	}

	// 3. Re-send same Idempotency-Key -> must return 200 OK with identical transaction ID
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", map[string]string{
		"Idempotency-Key": idemKey,
	}, createPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate Idempotency-Key status = %d, want 200", resp.StatusCode)
	}
	var dupTx TransactionResponse
	_ = json.Unmarshal(body, &dupTx)
	if dupTx.ID != createdTx.ID {
		t.Fatalf("duplicate Idempotency-Key returned ID %s, want %s", dupTx.ID, createdTx.ID)
	}

	// 4. Create an ADVANCE (立替) transaction with PAYABLE settlement
	advancePayload := map[string]any{
		"tx_date":           "2026-11-03",
		"kind":              "EXPENSE",
		"expense_type":      "VARIABLE",
		"category_id":       groceriesCatID,
		"title":             "택시비 대납",
		"gross_amount":      1500,
		"points_used":       0,
		"payment_method_id": advanceID,
		"settlements": []map[string]any{
			{"peer_name": "에이키", "direction": "PAYABLE", "amount": 1500, "method": "PayPay"},
		},
	}
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", nil, advancePayload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST ADVANCE transaction status = %d, body = %s", resp.StatusCode, string(body))
	}

	// 4b. Create a FIXED transaction in a second category to verify category_breakdown & category_id filtering
	rentCatID := catWrap.Items[1].ID
	for _, cat := range catWrap.Items {
		if cat.Name == "RENT" {
			rentCatID = cat.ID
			break
		}
	}
	fixedPayload := map[string]any{
		"tx_date":           "2026-11-01",
		"kind":              "EXPENSE",
		"expense_type":      "FIXED",
		"category_id":       rentCatID,
		"title":             "11월 집세",
		"gross_amount":      85000,
		"points_used":       0,
		"payment_method_id": creditCardID,
	}
	resp, body = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", nil, fixedPayload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST FIXED transaction status = %d, body = %s", resp.StatusCode, string(body))
	}

	// 5. Verify Monthly Summary (my_share basis + daily cumulative + outstanding settlements + category_breakdown)
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/ledger/summary?year=2026&month=11", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ledger/summary status = %d, body = %s", resp.StatusCode, string(body))
	}
	var summary MonthlySummaryResponse
	_ = json.Unmarshal(body, &summary)
	// Variable total = 600 (my_share from ramen) + 1500 (my_share from taxi advance) = 2100
	if summary.VariableTotal != 2100 {
		t.Errorf("summary.VariableTotal = %d, want 2100", summary.VariableTotal)
	}
	if summary.FixedTotal != 85000 {
		t.Errorf("summary.FixedTotal = %d, want 85000", summary.FixedTotal)
	}
	if summary.PointsSaved != 600 {
		t.Errorf("summary.PointsSaved = %d, want 600", summary.PointsSaved)
	}
	if summary.ReceivableOutstanding != 2400 || summary.PayableOutstanding != 1500 {
		t.Errorf("unexpected outstanding: receivable=%d payable=%d", summary.ReceivableOutstanding, summary.PayableOutstanding)
	}
	if len(summary.DailyVariableCumulative) != 30 {
		t.Errorf("expected 30 days in November cumulative series, got %d", len(summary.DailyVariableCumulative))
	}
	if len(summary.CategoryBreakdown) != 2 {
		t.Fatalf("expected 2 categories in breakdown, got %d", len(summary.CategoryBreakdown))
	}
	// Ordered by total_my_share DESC -> RENT (85000) first, GROCERIES (2100) second
	if summary.CategoryBreakdown[0].CategoryID != rentCatID || summary.CategoryBreakdown[0].FixedAmount != 85000 || summary.CategoryBreakdown[0].FixedTxCount != 1 {
		t.Errorf("unexpected breakdown[0]: %+v", summary.CategoryBreakdown[0])
	}
	if summary.CategoryBreakdown[1].CategoryID != groceriesCatID || summary.CategoryBreakdown[1].VariableAmount != 2100 || summary.CategoryBreakdown[1].VariableTxCount != 2 {
		t.Errorf("unexpected breakdown[1]: %+v", summary.CategoryBreakdown[1])
	}

	// 5b. Verify GET /api/v1/transactions?category_id= filtering and validation
	resp, body = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions?year=2026&month=11&category_id="+strconv.Itoa(int(groceriesCatID)), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /transactions?category_id status = %d, body = %s", resp.StatusCode, string(body))
	}
	var filteredWrap struct {
		Items []TransactionResponse `json:"items"`
	}
	_ = json.Unmarshal(body, &filteredWrap)
	if len(filteredWrap.Items) != 2 {
		t.Errorf("expected 2 transactions for groceries category, got %d", len(filteredWrap.Items))
	}

	resp, _ = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions?year=2026&month=11&category_id=0", nil, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("GET /transactions?category_id=0 status = %d, want 422", resp.StatusCode)
	}

	// 6. Settle one settlement and verify 409 Conflict when trying to edit amounts on that transaction
	firstSettlementID := createdTx.Settlements[0].ID
	resp, body = doJSONRequest(t, client, http.MethodPatch, ts.URL+"/api/v1/settlements/"+firstSettlementID, nil, map[string]any{
		"settled": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /settlements status = %d, body = %s", resp.StatusCode, string(body))
	}

	// Attempting to change gross_amount on a transaction with a settled item must return 409 Conflict
	resp, _ = doJSONRequest(t, client, http.MethodPatch, ts.URL+"/api/v1/transactions/"+createdTx.ID, nil, map[string]any{
		"gross_amount": 4000,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when modifying settled transaction amount, got %d", resp.StatusCode)
	}

	// Modifying only the title/memo on a settled transaction is still allowed
	resp, body = doJSONRequest(t, client, http.MethodPatch, ts.URL+"/api/v1/transactions/"+createdTx.ID, nil, map[string]any{
		"title": "라멘회 (수정됨)",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK when modifying title only, got %d: %s", resp.StatusCode, string(body))
	}

	// 7. Verify deprecated IRREGULAR expense_type returns 422 on POST and GET filter
	resp, _ = doJSONRequest(t, client, http.MethodPost, ts.URL+"/api/v1/transactions", nil, map[string]any{
		"tx_date":           "2026-11-04",
		"kind":              "EXPENSE",
		"expense_type":      "IRREGULAR",
		"category_id":       groceriesCatID,
		"title":             "비정기 테스트",
		"gross_amount":      1000,
		"points_used":       0,
		"payment_method_id": creditCardID,
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /transactions with IRREGULAR status = %d, want 422", resp.StatusCode)
	}

	resp, _ = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions?year=2026&month=11&expense_type=IRREGULAR", nil, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("GET /transactions?expense_type=IRREGULAR status = %d, want 422", resp.StatusCode)
	}

	// 8. Soft-delete transaction and verify DELETE outbox entry + 404 on subsequent GET
	resp, _ = doJSONRequest(t, client, http.MethodDelete, ts.URL+"/api/v1/transactions/"+createdTx.ID, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /transactions status = %d, want 204", resp.StatusCode)
	}

	resp, _ = doJSONRequest(t, client, http.MethodGet, ts.URL+"/api/v1/transactions/"+createdTx.ID, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted transaction status = %d, want 404", resp.StatusCode)
	}

	var deleteOutboxCount int
	_ = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM sheet_sync_outbox WHERE op = 'DELETE'`).Scan(&deleteOutboxCount)
	if deleteOutboxCount != 1 {
		t.Errorf("expected 1 DELETE entry in sheet_sync_outbox, got %d", deleteOutboxCount)
	}
}

