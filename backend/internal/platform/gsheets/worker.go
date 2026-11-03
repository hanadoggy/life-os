package gsheets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	outboxBatchSize = 50
	maxBackoffDur   = 6 * time.Hour
)

// WorkerConfig holds configuration for the Google Sheets Write-behind Outbox Worker.
type WorkerConfig struct {
	Enabled        bool
	SpreadsheetIDs map[int]string
	MonthTabFormat string
	TxIDColumn     string
	AppLogTab      string
	Location       *time.Location
}

// Worker processes pending sheet_sync_outbox records in a single goroutine.
type Worker struct {
	cfg     WorkerConfig
	queries *db.Queries
	writer  SheetWriter
	wakeCh  <-chan struct{}
	logger  *slog.Logger
}

// NewWorker creates a new Google Sheets Write-behind Worker.
func NewWorker(
	cfg WorkerConfig,
	queries *db.Queries,
	writer SheetWriter,
	wakeCh <-chan struct{},
	logger *slog.Logger,
) *Worker {
	if cfg.Location == nil {
		cfg.Location = time.FixedZone("JST", 9*3600)
	}
	if cfg.MonthTabFormat == "" {
		cfg.MonthTabFormat = "{Month}"
	}
	if cfg.TxIDColumn == "" {
		cfg.TxIDColumn = "Z"
	}
	return &Worker{
		cfg:     cfg,
		queries: queries,
		writer:  writer,
		wakeCh:  wakeCh,
		logger:  logger,
	}
}

// Run executes the single-goroutine worker loop (initial sweep + wake channel + 1-minute ticker).
func (w *Worker) Run(ctx context.Context) {
	if !w.cfg.Enabled || w.writer == nil {
		w.logger.Info("gsheets_worker_disabled")
		return
	}

	w.logger.Info("gsheets_worker_started",
		slog.String("tab_format", w.cfg.MonthTabFormat),
		slog.String("tx_id_col", w.cfg.TxIDColumn),
	)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// 1. Initial sweep on startup
	w.DrainOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("gsheets_worker_stopping")
			return
		case <-w.wakeCh:
			w.DrainOnce(ctx)
		case <-ticker.C:
			w.DrainOnce(ctx)
		}
	}
}

// DrainOnce fetches and processes pending outbox items, coalescing multiple items for the same transaction_id.
func (w *Worker) DrainOnce(ctx context.Context) {
	if !w.cfg.Enabled || w.writer == nil {
		return
	}

	items, err := w.queries.ListPendingSheetSyncOutbox(ctx, outboxBatchSize)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("gsheets_list_outbox_failed", slog.String("error", err.Error()))
		}
		return
	}
	if len(items) == 0 {
		return
	}

	// Group pending outbox IDs by transaction_id while preserving first-seen order
	type txGroup struct {
		txID      pgtype.UUID
		outboxIDs []int64
		latestOp  string
		maxTries  int32
	}

	var order []pgtype.UUID
	groups := make(map[pgtype.UUID]*txGroup)
	for _, item := range items {
		g, exists := groups[item.TransactionID]
		if !exists {
			g = &txGroup{
				txID: item.TransactionID,
			}
			groups[item.TransactionID] = g
			order = append(order, item.TransactionID)
		}
		g.outboxIDs = append(g.outboxIDs, item.ID)
		g.latestOp = item.Op
		if item.Attempts > g.maxTries {
			g.maxTries = item.Attempts
		}
	}

	for _, txID := range order {
		if ctx.Err() != nil {
			return
		}
		g := groups[txID]
		if err := w.processTransactionGroup(ctx, g.txID, g.latestOp); err != nil {
			w.recordFailure(ctx, g.outboxIDs, g.maxTries, err)
			continue
		}
		if err := w.queries.MarkSheetSyncOutboxProcessed(ctx, g.outboxIDs); err != nil {
			w.logger.Error("gsheets_mark_processed_failed",
				slog.String("tx_id", formatUUID(g.txID)),
				slog.String("error", err.Error()),
			)
		}
	}
}

func (w *Worker) processTransactionGroup(ctx context.Context, txID pgtype.UUID, latestOp string) error {
	txIDStr := formatUUID(txID)

	// Check current live state in v_ledger_entries
	entry, err := w.queries.GetLedgerEntryByID(ctx, txID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Transaction is soft-deleted (or doesn't exist in active view) -> perform DELETE
			return w.syncDelete(ctx, txID, txIDStr)
		}
		return fmt.Errorf("get ledger entry: %w", err)
	}

	if latestOp == "DELETE" {
		return w.syncDelete(ctx, txID, txIDStr)
	}

	return w.syncUpsert(ctx, entry, txIDStr)
}

func (w *Worker) syncUpsert(ctx context.Context, entry db.VLedgerEntry, txIDStr string) error {
	year := entry.TxDate.Time.Year()
	month := int(entry.TxDate.Time.Month())

	spreadsheetID, ok := w.cfg.SpreadsheetIDs[year]
	if !ok || spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet ID configured for year %d", year)
	}

	targetTab, err := w.writer.ResolveMonthTabName(ctx, spreadsheetID, w.cfg.MonthTabFormat, month)
	if err != nil {
		return fmt.Errorf("resolve month tab for %d/%02d: %w", year, month, err)
	}
	targetLocation := FormatSheetLocation(year, targetTab)

	// If the transaction previously lived in a different year/tab (e.g., tx_date month changed),
	// delete it from the old tab first.
	if entry.SheetLocation.Valid && entry.SheetLocation.String != "" && entry.SheetLocation.String != targetLocation {
		if oldYear, oldTab, parsed := ParseSheetLocation(entry.SheetLocation.String); parsed {
			if oldSheetID, hasOld := w.cfg.SpreadsheetIDs[oldYear]; hasOld && oldSheetID != "" {
				if err := w.writer.DeleteRow(ctx, oldSheetID, oldTab, w.cfg.TxIDColumn, txIDStr); err != nil {
					return fmt.Errorf("remove row from previous location %s: %w", entry.SheetLocation.String, err)
				}
			}
		}
	}

	cat, err := w.queries.GetCategoryByID(ctx, entry.CategoryID)
	if err != nil {
		return fmt.Errorf("get category %d: %w", entry.CategoryID, err)
	}

	expTypeCode := "VARIABLE"
	if entry.ExpenseType.Valid && entry.ExpenseType.String != "" {
		expTypeCode = entry.ExpenseType.String
	}

	memoStr := ""
	if entry.Memo.Valid {
		memoStr = entry.Memo.String
	}

	rowData := LedgerRowData{
		TxID:            txIDStr,
		TxDate:          entry.TxDate.Time.Format("2006-01-02"),
		Kind:            entry.Kind,
		ExpenseTypeCode: expTypeCode,
		CategoryCode:    cat.Name,
		Title:           entry.Title,
		GrossAmount:     entry.GrossAmount,
		PointsUsed:      entry.PointsUsed,
		NetAmount:       entry.NetAmount.Int32,
		ReceivableTotal: entry.ReceivableTotal,
		MyShare:         entry.MyShare,
		Memo:            memoStr,
	}

	if err := w.writer.UpsertRow(ctx, spreadsheetID, targetTab, w.cfg.TxIDColumn, rowData); err != nil {
		return fmt.Errorf("upsert row to %s: %w", targetLocation, err)
	}

	if err := w.queries.UpdateTransactionSheetSyncState(ctx, db.UpdateTransactionSheetSyncStateParams{
		ID:            entry.ID,
		SheetLocation: pgtype.Text{String: targetLocation, Valid: true},
	}); err != nil {
		return fmt.Errorf("update transaction sheet_location: %w", err)
	}

	nowStr := time.Now().In(w.cfg.Location).Format(time.RFC3339)
	summary := fmt.Sprintf("%s %s ¥%d (%s)", rowData.TxDate, rowData.Title, rowData.MyShare, targetTab)
	if logErr := w.writer.AppendAppLog(ctx, spreadsheetID, w.cfg.AppLogTab, nowStr, "UPSERT", txIDStr, summary); logErr != nil {
		w.logger.Warn("gsheets_append_app_log_failed",
			slog.String("tx_id", txIDStr),
			slog.String("error", logErr.Error()),
		)
	}

	return nil
}

func (w *Worker) syncDelete(ctx context.Context, txID pgtype.UUID, txIDStr string) error {
	meta, err := w.queries.GetTransactionSyncMetaByID(ctx, txID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get transaction sync meta: %w", err)
	}

	var year int
	var tabName string

	if meta.SheetLocation.Valid && meta.SheetLocation.String != "" {
		if y, t, ok := ParseSheetLocation(meta.SheetLocation.String); ok {
			year = y
			tabName = t
		}
	}
	if year == 0 {
		year = meta.TxDate.Time.Year()
	}
	spreadsheetID, ok := w.cfg.SpreadsheetIDs[year]
	if !ok || spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet ID configured for year %d", year)
	}

	if tabName == "" {
		month := int(meta.TxDate.Time.Month())
		resolvedTab, err := w.writer.ResolveMonthTabName(ctx, spreadsheetID, w.cfg.MonthTabFormat, month)
		if err != nil {
			return fmt.Errorf("resolve month tab for delete %d/%02d: %w", year, month, err)
		}
		tabName = resolvedTab
	}

	if err := w.writer.DeleteRow(ctx, spreadsheetID, tabName, w.cfg.TxIDColumn, txIDStr); err != nil {
		return fmt.Errorf("delete row from %d/%s: %w", year, tabName, err)
	}

	_ = w.queries.UpdateTransactionSheetSyncState(ctx, db.UpdateTransactionSheetSyncStateParams{
		ID:            txID,
		SheetLocation: pgtype.Text{},
	})

	nowStr := time.Now().In(w.cfg.Location).Format(time.RFC3339)
	summary := fmt.Sprintf("deleted from %s", tabName)
	if logErr := w.writer.AppendAppLog(ctx, spreadsheetID, w.cfg.AppLogTab, nowStr, "DELETE", txIDStr, summary); logErr != nil {
		w.logger.Warn("gsheets_append_app_log_failed",
			slog.String("tx_id", txIDStr),
			slog.String("error", logErr.Error()),
		)
	}

	return nil
}

func (w *Worker) recordFailure(ctx context.Context, outboxIDs []int64, prevAttempts int32, syncErr error) {
	nextAttempt := ComputeNextAttempt(prevAttempts+1, time.Now())
	errMsg := syncErr.Error()

	if prevAttempts+1 >= 10 {
		w.logger.Error("gsheets_outbox_repeated_failure",
			slog.Int("attempts", int(prevAttempts+1)),
			slog.String("error", errMsg),
		)
	} else {
		w.logger.Warn("gsheets_outbox_sync_failed",
			slog.Int("attempts", int(prevAttempts+1)),
			slog.Time("next_attempt_at", nextAttempt),
			slog.String("error", errMsg),
		)
	}

	for _, id := range outboxIDs {
		_ = w.queries.MarkSheetSyncOutboxFailed(ctx, db.MarkSheetSyncOutboxFailedParams{
			ID:            id,
			LastError:     pgtype.Text{String: errMsg, Valid: true},
			NextAttemptAt: nextAttempt,
		})
	}
}

// ComputeNextAttempt calculates now + min(2^attempts minutes, 6 hours).
func ComputeNextAttempt(attempts int32, now time.Time) time.Time {
	if attempts < 1 {
		attempts = 1
	}
	exp := math.Pow(2, float64(attempts))
	dur := time.Duration(exp) * time.Minute
	if dur > maxBackoffDur || dur <= 0 {
		dur = maxBackoffDur
	}
	return now.Add(dur)
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
