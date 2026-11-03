package gsheets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FakeSheetWriter is an in-memory implementation of SheetWriter for testing idempotency,
// month migration, delete, and app_log appending without external API calls.
type FakeSheetWriter struct {
	mu      sync.Mutex
	tabs    map[string]map[string][]LedgerRowData // spreadsheetID -> tabName -> rows
	appLogs map[string][]string                   // spreadsheetID -> log lines
	failErr error
}

func NewFakeSheetWriter() *FakeSheetWriter {
	return &FakeSheetWriter{
		tabs:    make(map[string]map[string][]LedgerRowData),
		appLogs: make(map[string][]string),
	}
}

func (f *FakeSheetWriter) SetFailError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failErr = err
}

func (f *FakeSheetWriter) ResolveMonthTabName(_ context.Context, _, tabFormat string, month int) (string, error) {
	return FormatMonthTabName(tabFormat, month), nil
}

func (f *FakeSheetWriter) UpsertRow(_ context.Context, spreadsheetID, tabName, _ string, row LedgerRowData) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	if f.tabs[spreadsheetID] == nil {
		f.tabs[spreadsheetID] = make(map[string][]LedgerRowData)
	}
	rows := f.tabs[spreadsheetID][tabName]
	for i, existing := range rows {
		if existing.TxID == row.TxID {
			rows[i] = row
			f.tabs[spreadsheetID][tabName] = rows
			return nil
		}
	}
	f.tabs[spreadsheetID][tabName] = append(rows, row)
	return nil
}

func (f *FakeSheetWriter) DeleteRow(_ context.Context, spreadsheetID, tabName, _, txID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	if f.tabs[spreadsheetID] == nil {
		return nil
	}
	rows := f.tabs[spreadsheetID][tabName]
	var kept []LedgerRowData
	for _, r := range rows {
		if r.TxID != txID {
			kept = append(kept, r)
		}
	}
	f.tabs[spreadsheetID][tabName] = kept
	return nil
}

func (f *FakeSheetWriter) AppendAppLog(_ context.Context, spreadsheetID, _, timestamp, op, txID, summary string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appLogs[spreadsheetID] = append(f.appLogs[spreadsheetID], fmt.Sprintf("%s|%s|%s|%s", timestamp, op, txID, summary))
	return nil
}

func (f *FakeSheetWriter) ReadTabRows(_ context.Context, _, _, _ string) ([][]string, error) {
	return nil, nil
}

func (f *FakeSheetWriter) BatchWriteTxIDs(_ context.Context, _, _, _ string, _ map[int]string) error {
	return nil
}

func (f *FakeSheetWriter) GetRows(spreadsheetID, tabName string) []LedgerRowData {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tabs[spreadsheetID] == nil {
		return nil
	}
	src := f.tabs[spreadsheetID][tabName]
	cp := make([]LedgerRowData, len(src))
	copy(cp, src)
	return cp
}

func TestLayoutAndParsing(t *testing.T) {
	// 1. Tab name formatting
	if got := FormatMonthTabName("{Month}", 1); got != "January" {
		t.Fatalf("expected January, got %q", got)
	}
	if got := FormatMonthTabName("{Month}", 11); got != "November" {
		t.Fatalf("expected November, got %q", got)
	}
	if m, ok := ParseMonthFromTabName("February"); !ok || m != 2 {
		t.Fatalf("expected month 2 for February, got %d (%v)", m, ok)
	}

	// 2. Category fuzzy normalization (A..F sheet variations -> 15 LifeOS codes)
	catTests := map[string]string{
		"食料品":       "GROCERIES",
		"食用":        "GROCERIES",
		"食費":        "GROCERIES",
		"外食":        "GROCERIES",
		"通信費":       "COMMUNICATION",
		"インターネット":   "COMMUNICATION",
		"水道光熱費":     "UTILITIES",
		"電気・ガス":     "UTILITIES",
		"日用品":       "HOUSEHOLD",
		"生活用品":      "HOUSEHOLD",
		"交際費":       "SOCIAL",
		"ゲーム":       "GAMING",
		"文化・娯楽":     "CULTURE",
		"趣味":        "CULTURE",
		"家賃":        "RENT",
		"交通費":       "TRANSPORT",
		"サブスク":      "SUBSCRIPTION",
		"税金・ふるさと納税": "PUBLIC_FEES",
		"投資":        "INVESTMENT",
		"衣類・美容":     "CLOTHING",
		"家具家電":      "FURNITURE_ELECTRONICS",
		"自己啓発":      "SELF_DEVELOPMENT",
	}
	for input, want := range catTests {
		got, matched := NormalizeCategoryCode(input)
		if !matched || got != want {
			t.Errorf("NormalizeCategoryCode(%q) = (%q, %v), want (%q, true)", input, got, matched, want)
		}
	}

	// 3. ParseSheetRow (Columns A..F: Date, Fixed/Variable, Category, Title, Amount, Memo)
	cells := []string{"2025/11/03", "変動費", "食用", "成城石井", "¥2,480", "480ポイント使用"}
	parsed, skip, err := ParseSheetRow(12, cells, 25, 2025, 11, true)
	if err != nil || skip {
		t.Fatalf("unexpected error=%v skip=%v", err, skip)
	}
	if parsed.Kind != "EXPENSE" || parsed.ExpenseType != "VARIABLE" {
		t.Errorf("expected EXPENSE/VARIABLE, got %s/%s", parsed.Kind, parsed.ExpenseType)
	}
	if parsed.CategoryCode != "GROCERIES" {
		t.Errorf("expected GROCERIES, got %s", parsed.CategoryCode)
	}
	if parsed.Title != "成城石井" {
		t.Errorf("expected 成城石井, got %s", parsed.Title)
	}
	if parsed.PointsUsed != 480 || parsed.GrossAmount != 2480 {
		t.Errorf("expected points=480 gross=2480, got points=%d gross=%d", parsed.PointsUsed, parsed.GrossAmount)
	}

	// Verify S&P500 is NOT parsed as points, while 全てポイント使用 IS parsed as full amount
	if got := ExtractExplicitPointsFromMemo("S&P500（NISA 100000円, iDeco 20000円）", 120000); got != 0 {
		t.Errorf("expected 0 points for S&P500, got %d", got)
	}
	if got := ExtractExplicitPointsFromMemo("ドリンク、全てポイント使用", 899); got != 899 {
		t.Errorf("expected 899 points for 全てポイント使用, got %d", got)
	}
	if got := ExtractExplicitPointsFromMemo("午後の紅茶、ポイント2580円使用", 6673); got != 2580 {
		t.Errorf("expected 2580 points for ポイント2580円使用, got %d", got)
	}

	// 4. FormatRowValues (A..F output)
	vals := FormatRowValues(LedgerRowData{
		TxID:            "test-uuid",
		TxDate:          "2026-11-03",
		Kind:            "EXPENSE",
		ExpenseTypeCode: "VARIABLE",
		CategoryCode:    "GROCERIES",
		Title:           "ラーメン会",
		GrossAmount:     3600,
		PointsUsed:      600,
		NetAmount:       3000,
		ReceivableTotal: 2000,
		MyShare:         1000,
		Memo:            "エイキ・テツ",
	})
	if len(vals) != 6 {
		t.Fatalf("expected 6 columns (A..F), got %d", len(vals))
	}
	if vals[1] != "変動費" || vals[2] != "食料品" || vals[4] != int32(1000) {
		t.Errorf("unexpected formatted row: %#v", vals)
	}
	wantMemo := "エイキ・テツ (総額 3,600 / P 600 / 精算 2,000)"
	if vals[5] != wantMemo {
		t.Errorf("memo = %q, want %q", vals[5], wantMemo)
	}
}

func TestWorkerIdempotencyMonthMoveAndDeleteIntegration(t *testing.T) {
	testDSN := os.Getenv("TEST_DATABASE_URL")
	if testDSN == "" {
		testDSN = "postgres://lifeos:lifeos_dev_password@localhost:5432/lifeos_test?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Skipf("skipping integration test: pool create failed: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping integration test: db ping failed: %v", err)
	}

	_, _ = pool.Exec(ctx, "TRUNCATE TABLE sheet_sync_outbox, settlements, transactions RESTART IDENTITY CASCADE")

	queries := db.New(pool)
	fakeWriter := NewFakeSheetWriter()
	wakeCh := make(chan struct{}, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	worker := NewWorker(WorkerConfig{
		Enabled: true,
		SpreadsheetIDs: map[int]string{
			2025: "sheet-2025",
			2026: "sheet-2026",
		},
		MonthTabFormat: "{Month}",
		TxIDColumn:     "Z",
		AppLogTab:      "app_log",
	}, queries, fakeWriter, wakeCh, logger)

	cats, err := queries.ListActiveCategories(ctx)
	if err != nil || len(cats) == 0 {
		t.Fatalf("list categories: %v", err)
	}
	pms, err := queries.ListActivePaymentMethods(ctx)
	if err != nil || len(pms) == 0 {
		t.Fatalf("list payment methods: %v", err)
	}

	// 1. Insert a November 2026 transaction + two UPSERT outbox records (test coalescing & idempotency)
	novDate := pgtype.Date{Time: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), Valid: true}
	txID, err := queries.CreateTransaction(ctx, db.CreateTransactionParams{
		TxDate:          novDate,
		Kind:            "EXPENSE",
		ExpenseType:     pgtype.Text{String: "VARIABLE", Valid: true},
		CategoryID:      cats[0].ID,
		Title:           "成城石井",
		GrossAmount:     2500,
		PointsUsed:      500,
		PaymentMethodID: pms[0].ID,
		Memo:            pgtype.Text{String: "テスト", Valid: true},
		Metadata:        []byte("{}"),
		Source:          "APP",
	})
	if err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	_, _ = queries.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{TransactionID: txID, Op: "UPSERT"})
	_, _ = queries.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{TransactionID: txID, Op: "UPSERT"})

	worker.DrainOnce(ctx)

	novRows := fakeWriter.GetRows("sheet-2026", "November")
	if len(novRows) != 1 {
		t.Fatalf("expected 1 row in 2026/November, got %d", len(novRows))
	}
	if novRows[0].MyShare != 2000 || novRows[0].Title != "成城石井" {
		t.Errorf("unexpected row data: %+v", novRows[0])
	}

	// Re-enqueue UPSERT and drain again -> must update in-place without duplicating
	_, _ = queries.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{TransactionID: txID, Op: "UPSERT"})
	worker.DrainOnce(ctx)
	if got := len(fakeWriter.GetRows("sheet-2026", "November")); got != 1 {
		t.Fatalf("expected idempotent 1 row after retry, got %d", got)
	}

	// 2. Move transaction date from November 2026 -> December 2026
	decDate := pgtype.Date{Time: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	_, err = queries.UpdateTransaction(ctx, db.UpdateTransactionParams{
		ID:              txID,
		TxDate:          decDate,
		Kind:            "EXPENSE",
		ExpenseType:     pgtype.Text{String: "VARIABLE", Valid: true},
		CategoryID:      cats[0].ID,
		Title:           "成城石井 (12月移動)",
		GrossAmount:     2500,
		PointsUsed:      500,
		PaymentMethodID: pms[0].ID,
		Memo:            pgtype.Text{String: "テスト", Valid: true},
		Metadata:        []byte("{}"),
	})
	if err != nil {
		t.Fatalf("update transaction date: %v", err)
	}
	_, _ = queries.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{TransactionID: txID, Op: "UPSERT"})

	worker.DrainOnce(ctx)

	if got := len(fakeWriter.GetRows("sheet-2026", "November")); got != 0 {
		t.Fatalf("expected 0 rows in old tab November after month move, got %d", got)
	}
	decRows := fakeWriter.GetRows("sheet-2026", "December")
	if len(decRows) != 1 || decRows[0].Title != "成城石井 (12月移動)" {
		t.Fatalf("expected 1 moved row in December, got %+v", decRows)
	}

	// 3. Soft-delete transaction -> must remove row from December tab
	_, _ = queries.SoftDeleteTransaction(ctx, txID)
	_, _ = queries.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{TransactionID: txID, Op: "DELETE"})

	worker.DrainOnce(ctx)

	if got := len(fakeWriter.GetRows("sheet-2026", "December")); got != 0 {
		t.Fatalf("expected 0 rows in December after delete, got %d", got)
	}

	// Verify no pending outbox records remain
	pending, err := queries.ListPendingSheetSyncOutbox(ctx, 10)
	if err != nil && err != pgx.ErrNoRows {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending outbox items, got %d", len(pending))
	}
}

