package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service implements domain logic and persistence for the Ledger module.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	loc     *time.Location
	wakeCh  chan<- struct{}
}

// NewService creates a new Ledger Service.
func NewService(pool *pgxpool.Pool, loc *time.Location, wakeCh chan<- struct{}) *Service {
	if loc == nil {
		loc = time.FixedZone("JST", 9*3600)
	}
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		loc:     loc,
		wakeCh:  wakeCh,
	}
}

func (s *Service) notifyWorker() {
	if s.wakeCh == nil {
		return
	}
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

// ValidateTransactionAccounting validates domain and accounting invariants for a transaction and its settlements.
// This function is pure and unit-tested directly without requiring a database connection.
func ValidateTransactionAccounting(
	title string,
	kind string,
	expenseType *string,
	categoryID int32,
	paymentMethodID int32,
	isAdvancePayment bool,
	grossAmount int32,
	pointsUsed int32,
	metadata json.RawMessage,
	settlements []SettlementInput,
) error {
	var fields []httpx.FieldError

	trimmedTitle := strings.TrimSpace(title)
	if l := utf8.RuneCountInString(trimmedTitle); l < 1 || l > 200 {
		fields = append(fields, httpx.FieldError{
			Field:   "title",
			Message: "must be between 1 and 200 characters",
		})
	}

	switch kind {
	case KindExpense:
		if expenseType == nil || !isValidExpenseType(*expenseType) {
			fields = append(fields, httpx.FieldError{
				Field:   "expense_type",
				Message: "must be FIXED, VARIABLE, or IRREGULAR when kind is EXPENSE",
			})
		}
	case KindIncome:
		if expenseType != nil && *expenseType != "" {
			fields = append(fields, httpx.FieldError{
				Field:   "expense_type",
				Message: "must be null when kind is INCOME",
			})
		}
		if pointsUsed != 0 {
			fields = append(fields, httpx.FieldError{
				Field:   "points_used",
				Message: "must be 0 when kind is INCOME",
			})
		}
	default:
		fields = append(fields, httpx.FieldError{
			Field:   "kind",
			Message: "must be EXPENSE or INCOME",
		})
	}

	if categoryID <= 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "category_id",
			Message: "must be a positive integer",
		})
	}
	if paymentMethodID <= 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "payment_method_id",
			Message: "must be a positive integer",
		})
	}

	if grossAmount < 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "gross_amount",
			Message: "must be >= 0",
		})
	}
	if pointsUsed < 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "points_used",
			Message: "must be >= 0",
		})
	}
	if grossAmount >= 0 && pointsUsed > grossAmount {
		fields = append(fields, httpx.FieldError{
			Field:   "points_used",
			Message: "points_used must be <= gross_amount",
		})
	}

	if len(metadata) > 0 {
		var js map[string]any
		if err := json.Unmarshal(metadata, &js); err != nil {
			fields = append(fields, httpx.FieldError{
				Field:   "metadata",
				Message: "must be a valid JSON object",
			})
		}
	}

	netAmount := grossAmount - pointsUsed
	var receivableTotal int32
	var payableTotal int32
	var payableCount int

	for i, st := range settlements {
		peer := strings.TrimSpace(st.PeerName)
		if l := utf8.RuneCountInString(peer); l < 1 || l > 50 {
			fields = append(fields, httpx.FieldError{
				Field:   fmt.Sprintf("settlements[%d].peer_name", i),
				Message: "must be between 1 and 50 characters",
			})
		}
		if st.Amount <= 0 {
			fields = append(fields, httpx.FieldError{
				Field:   fmt.Sprintf("settlements[%d].amount", i),
				Message: "must be > 0",
			})
		}
		switch st.Direction {
		case DirectionReceivable:
			receivableTotal += st.Amount
		case DirectionPayable:
			payableTotal += st.Amount
			payableCount++
		default:
			fields = append(fields, httpx.FieldError{
				Field:   fmt.Sprintf("settlements[%d].direction", i),
				Message: "must be RECEIVABLE or PAYABLE",
			})
		}
	}

	// Accounting rule 1: RECEIVABLE total <= net_amount (so my_share >= 0)
	if receivableTotal > netAmount {
		fields = append(fields, httpx.FieldError{
			Field:   "settlements",
			Message: fmt.Sprintf("RECEIVABLE total (%d) must not exceed net_amount (%d)", receivableTotal, netAmount),
		})
	}

	// Accounting rule 2: ADVANCE (立替) payment method <-> exactly 1 PAYABLE settlement matching net_amount
	if isAdvancePayment {
		if payableCount != 1 || receivableTotal > 0 || payableTotal != netAmount {
			fields = append(fields, httpx.FieldError{
				Field:   "settlements",
				Message: "ADVANCE (立替) payment method requires exactly 1 PAYABLE settlement equal to net_amount and no RECEIVABLE settlements",
			})
		}
	} else if payableCount > 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "payment_method_id",
			Message: "PAYABLE settlement requires payment method ADVANCE (立替)",
		})
	}

	if len(fields) > 0 {
		return newValidationError(fields[0].Message, fields...)
	}
	return nil
}

// CreateTransaction creates a new transaction, optional settlements, and a sheet_sync_outbox entry atomically.
// Returns (response, created=false, nil) if an existing transaction with the same Idempotency-Key is found.
func (s *Service) CreateTransaction(
	ctx context.Context,
	idempotencyKey *pgtype.UUID,
	req CreateTransactionRequest,
) (TransactionResponse, bool, error) {
	if idempotencyKey != nil && idempotencyKey.Valid {
		existing, err := s.queries.GetLedgerEntryByIdempotencyKey(ctx, *idempotencyKey)
		if err == nil {
			resp, err := s.buildTransactionResponse(ctx, s.queries, existing)
			return resp, false, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, false, fmt.Errorf("check idempotency key: %w", err)
		}
	}

	var txDate pgtype.Date
	if strings.TrimSpace(req.TxDate) == "" {
		nowJST := time.Now().In(s.loc)
		today := time.Date(nowJST.Year(), nowJST.Month(), nowJST.Day(), 0, 0, 0, 0, time.UTC)
		txDate = pgtype.Date{Time: today, Valid: true}
	} else {
		parsedDate, err := parseDate(req.TxDate)
		if err != nil {
			return TransactionResponse{}, false, newValidationError(
				"tx_date must be YYYY-MM-DD",
				httpx.FieldError{Field: "tx_date", Message: "must be YYYY-MM-DD"},
			)
		}
		txDate = parsedDate
	}

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = KindExpense
	}

	cat, err := s.queries.GetCategoryByID(ctx, req.CategoryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, false, newValidationError(
				"category_id does not exist",
				httpx.FieldError{Field: "category_id", Message: "unknown category_id"},
			)
		}
		return TransactionResponse{}, false, fmt.Errorf("get category: %w", err)
	}
	if !cat.IsActive {
		return TransactionResponse{}, false, newValidationError(
			"category is inactive",
			httpx.FieldError{Field: "category_id", Message: "category is inactive"},
		)
	}

	pm, err := s.queries.GetPaymentMethodByID(ctx, req.PaymentMethodID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, false, newValidationError(
				"payment_method_id does not exist",
				httpx.FieldError{Field: "payment_method_id", Message: "unknown payment_method_id"},
			)
		}
		return TransactionResponse{}, false, fmt.Errorf("get payment method: %w", err)
	}
	if !pm.IsActive {
		return TransactionResponse{}, false, newValidationError(
			"payment method is inactive",
			httpx.FieldError{Field: "payment_method_id", Message: "payment method is inactive"},
		)
	}

	isAdvance := pm.Name == PaymentMethodAdvance
	if err := ValidateTransactionAccounting(
		req.Title,
		kind,
		req.ExpenseType,
		req.CategoryID,
		req.PaymentMethodID,
		isAdvance,
		req.GrossAmount,
		req.PointsUsed,
		req.Metadata,
		req.Settlements,
	); err != nil {
		return TransactionResponse{}, false, err
	}

	metaBytes := []byte("{}")
	if len(req.Metadata) > 0 {
		metaBytes = req.Metadata
	}

	var expType pgtype.Text
	if req.ExpenseType != nil && *req.ExpenseType != "" {
		expType = pgtype.Text{String: *req.ExpenseType, Valid: true}
	}

	var memo pgtype.Text
	if req.Memo != nil {
		memo = pgtype.Text{String: *req.Memo, Valid: true}
	}

	var idemKey pgtype.UUID
	if idempotencyKey != nil && idempotencyKey.Valid {
		idemKey = *idempotencyKey
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TransactionResponse{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	txID, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		TxDate:          txDate,
		Kind:            kind,
		ExpenseType:     expType,
		CategoryID:      req.CategoryID,
		Title:           strings.TrimSpace(req.Title),
		GrossAmount:     req.GrossAmount,
		PointsUsed:      req.PointsUsed,
		PaymentMethodID: req.PaymentMethodID,
		Memo:            memo,
		Metadata:        metaBytes,
		Source:          SourceApp,
		SourceRef:       pgtype.Text{},
		IdempotencyKey:  idemKey,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && idemKey.Valid {
			_ = tx.Rollback(ctx)
			existing, getErr := s.queries.GetLedgerEntryByIdempotencyKey(ctx, idemKey)
			if getErr == nil {
				resp, buildErr := s.buildTransactionResponse(ctx, s.queries, existing)
				return resp, false, buildErr
			}
		}
		return TransactionResponse{}, false, fmt.Errorf("insert transaction: %w", err)
	}

	for _, st := range req.Settlements {
		method := strings.TrimSpace(st.Method)
		if method == "" {
			method = DefaultSettleMethod
		}
		var stMemo pgtype.Text
		if st.Memo != nil {
			stMemo = pgtype.Text{String: *st.Memo, Valid: true}
		}
		if _, err := qtx.CreateSettlement(ctx, db.CreateSettlementParams{
			TransactionID: txID,
			PeerName:      strings.TrimSpace(st.PeerName),
			Direction:     st.Direction,
			Amount:        st.Amount,
			Method:        method,
			Memo:          stMemo,
		}); err != nil {
			return TransactionResponse{}, false, fmt.Errorf("insert settlement: %w", err)
		}
	}

	if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
		TransactionID: txID,
		Op:            OutboxOpUpsert,
	}); err != nil {
		return TransactionResponse{}, false, fmt.Errorf("enqueue sheet sync: %w", err)
	}

	entry, err := qtx.GetLedgerEntryByID(ctx, txID)
	if err != nil {
		return TransactionResponse{}, false, fmt.Errorf("load created ledger entry: %w", err)
	}
	resp, err := s.buildTransactionResponse(ctx, qtx, entry)
	if err != nil {
		return TransactionResponse{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TransactionResponse{}, false, fmt.Errorf("commit tx: %w", err)
	}

	s.notifyWorker()
	return resp, true, nil
}

// GetTransaction returns a single transaction by UUID along with its settlements.
func (s *Service) GetTransaction(ctx context.Context, id pgtype.UUID) (TransactionResponse, error) {
	entry, err := s.queries.GetLedgerEntryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, ErrNotFound
		}
		return TransactionResponse{}, fmt.Errorf("get ledger entry: %w", err)
	}
	return s.buildTransactionResponse(ctx, s.queries, entry)
}

// ListTransactions returns transactions for a given year/month and optional expense_type.
func (s *Service) ListTransactions(ctx context.Context, year, month int, expenseType *string) ([]TransactionResponse, error) {
	startDate, endDate, err := monthBounds(year, month)
	if err != nil {
		return nil, err
	}

	var expFilter pgtype.Text
	if expenseType != nil && strings.TrimSpace(*expenseType) != "" {
		et := strings.TrimSpace(*expenseType)
		if !isValidExpenseType(et) {
			return nil, newValidationError(
				"invalid expense_type filter",
				httpx.FieldError{Field: "expense_type", Message: "must be FIXED, VARIABLE, or IRREGULAR"},
			)
		}
		expFilter = pgtype.Text{String: et, Valid: true}
	}

	entries, err := s.queries.ListLedgerEntriesByMonth(ctx, db.ListLedgerEntriesByMonthParams{
		StartDate:   startDate,
		EndDate:     endDate,
		ExpenseType: expFilter,
	})
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	if len(entries) == 0 {
		return []TransactionResponse{}, nil
	}

	txIDs := make([]pgtype.UUID, len(entries))
	for i, e := range entries {
		txIDs[i] = e.ID
	}

	allSettlements, err := s.queries.ListSettlementsByTransactionIDs(ctx, txIDs)
	if err != nil {
		return nil, fmt.Errorf("list settlements for entries: %w", err)
	}

	settlementsByTx := make(map[pgtype.UUID][]SettlementResponse, len(entries))
	for _, st := range allSettlements {
		settlementsByTx[st.TransactionID] = append(settlementsByTx[st.TransactionID], mapSettlementResponse(st))
	}

	res := make([]TransactionResponse, len(entries))
	for i, e := range entries {
		sts := settlementsByTx[e.ID]
		if sts == nil {
			sts = []SettlementResponse{}
		}
		res[i] = mapLedgerEntryToResponse(e, sts)
	}
	return res, nil
}

// UpdateTransaction partially updates a transaction and optionally replaces its settlements.
func (s *Service) UpdateTransaction(ctx context.Context, id pgtype.UUID, req UpdateTransactionRequest) (TransactionResponse, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	current, err := qtx.GetLedgerEntryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, ErrNotFound
		}
		return TransactionResponse{}, fmt.Errorf("get ledger entry: %w", err)
	}

	settledCount, err := qtx.CountSettledByTransactionID(ctx, id)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("count settled: %w", err)
	}

	// Specification §5 & §6.2: If any settlement is already settled, reject changes to amounts or settlements.
	if settledCount > 0 {
		amountChanged := (req.GrossAmount != nil && *req.GrossAmount != current.GrossAmount) ||
			(req.PointsUsed != nil && *req.PointsUsed != current.PointsUsed)
		if amountChanged || req.Settlements != nil {
			return TransactionResponse{}, &ConflictError{
				Detail: "cannot modify transaction amounts or settlements when one or more settlements are already settled",
			}
		}
	}

	txDate := current.TxDate
	if req.TxDate != nil {
		parsedDate, err := parseDate(*req.TxDate)
		if err != nil {
			return TransactionResponse{}, newValidationError(
				"tx_date must be YYYY-MM-DD",
				httpx.FieldError{Field: "tx_date", Message: "must be YYYY-MM-DD"},
			)
		}
		txDate = parsedDate
	}

	kind := current.Kind
	if req.Kind != nil {
		kind = strings.TrimSpace(*req.Kind)
	}

	var expenseTypePtr *string
	if current.ExpenseType.Valid {
		v := current.ExpenseType.String
		expenseTypePtr = &v
	}
	if req.ExpenseType != nil {
		if *req.ExpenseType == "" {
			expenseTypePtr = nil
		} else {
			v := strings.TrimSpace(*req.ExpenseType)
			expenseTypePtr = &v
		}
	}
	if kind == KindIncome {
		expenseTypePtr = nil
	}

	categoryID := current.CategoryID
	if req.CategoryID != nil {
		categoryID = *req.CategoryID
		cat, err := qtx.GetCategoryByID(ctx, categoryID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return TransactionResponse{}, newValidationError(
					"category_id does not exist",
					httpx.FieldError{Field: "category_id", Message: "unknown category_id"},
				)
			}
			return TransactionResponse{}, fmt.Errorf("get category: %w", err)
		}
		if !cat.IsActive {
			return TransactionResponse{}, newValidationError(
				"category is inactive",
				httpx.FieldError{Field: "category_id", Message: "category is inactive"},
			)
		}
	}

	paymentMethodID := current.PaymentMethodID
	if req.PaymentMethodID != nil {
		paymentMethodID = *req.PaymentMethodID
	}
	pm, err := qtx.GetPaymentMethodByID(ctx, paymentMethodID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TransactionResponse{}, newValidationError(
				"payment_method_id does not exist",
				httpx.FieldError{Field: "payment_method_id", Message: "unknown payment_method_id"},
			)
		}
		return TransactionResponse{}, fmt.Errorf("get payment method: %w", err)
	}

	title := current.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}

	grossAmount := current.GrossAmount
	if req.GrossAmount != nil {
		grossAmount = *req.GrossAmount
	}

	pointsUsed := current.PointsUsed
	if req.PointsUsed != nil {
		pointsUsed = *req.PointsUsed
	}

	memo := current.Memo
	if req.Memo != nil {
		memo = pgtype.Text{String: *req.Memo, Valid: true}
	}

	metadata := current.Metadata
	if req.Metadata != nil {
		metadata = *req.Metadata
	}

	var settlementsToValidate []SettlementInput
	if req.Settlements != nil {
		settlementsToValidate = *req.Settlements
	} else {
		existingSts, err := qtx.ListSettlementsByTransactionID(ctx, id)
		if err != nil {
			return TransactionResponse{}, fmt.Errorf("list existing settlements: %w", err)
		}
		settlementsToValidate = make([]SettlementInput, len(existingSts))
		for i, st := range existingSts {
			var m *string
			if st.Memo.Valid {
				val := st.Memo.String
				m = &val
			}
			settlementsToValidate[i] = SettlementInput{
				PeerName:  st.PeerName,
				Direction: st.Direction,
				Amount:    st.Amount,
				Method:    st.Method,
				Memo:      m,
			}
		}
	}

	isAdvance := pm.Name == PaymentMethodAdvance
	if err := ValidateTransactionAccounting(
		title,
		kind,
		expenseTypePtr,
		categoryID,
		paymentMethodID,
		isAdvance,
		grossAmount,
		pointsUsed,
		metadata,
		settlementsToValidate,
	); err != nil {
		return TransactionResponse{}, err
	}

	var expType pgtype.Text
	if expenseTypePtr != nil && *expenseTypePtr != "" {
		expType = pgtype.Text{String: *expenseTypePtr, Valid: true}
	}

	if _, err := qtx.UpdateTransaction(ctx, db.UpdateTransactionParams{
		ID:              id,
		TxDate:          txDate,
		Kind:            kind,
		ExpenseType:     expType,
		CategoryID:      categoryID,
		Title:           title,
		GrossAmount:     grossAmount,
		PointsUsed:      pointsUsed,
		PaymentMethodID: paymentMethodID,
		Memo:            memo,
		Metadata:        metadata,
	}); err != nil {
		return TransactionResponse{}, fmt.Errorf("update transaction: %w", err)
	}

	if req.Settlements != nil {
		if err := qtx.DeleteSettlementsByTransactionID(ctx, id); err != nil {
			return TransactionResponse{}, fmt.Errorf("delete old settlements: %w", err)
		}
		for _, st := range *req.Settlements {
			method := strings.TrimSpace(st.Method)
			if method == "" {
				method = DefaultSettleMethod
			}
			var stMemo pgtype.Text
			if st.Memo != nil {
				stMemo = pgtype.Text{String: *st.Memo, Valid: true}
			}
			if _, err := qtx.CreateSettlement(ctx, db.CreateSettlementParams{
				TransactionID: id,
				PeerName:      strings.TrimSpace(st.PeerName),
				Direction:     st.Direction,
				Amount:        st.Amount,
				Method:        method,
				Memo:          stMemo,
			}); err != nil {
				return TransactionResponse{}, fmt.Errorf("insert updated settlement: %w", err)
			}
		}
	}

	if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
		TransactionID: id,
		Op:            OutboxOpUpsert,
	}); err != nil {
		return TransactionResponse{}, fmt.Errorf("enqueue sheet sync: %w", err)
	}

	updatedEntry, err := qtx.GetLedgerEntryByID(ctx, id)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("load updated entry: %w", err)
	}
	resp, err := s.buildTransactionResponse(ctx, qtx, updatedEntry)
	if err != nil {
		return TransactionResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TransactionResponse{}, fmt.Errorf("commit tx: %w", err)
	}

	s.notifyWorker()
	return resp, nil
}

// DeleteTransaction soft-deletes a transaction and records a DELETE operation in sheet_sync_outbox.
func (s *Service) DeleteTransaction(ctx context.Context, id pgtype.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	rows, err := qtx.SoftDeleteTransaction(ctx, id)
	if err != nil {
		return fmt.Errorf("soft delete transaction: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
		TransactionID: id,
		Op:            OutboxOpDelete,
	}); err != nil {
		return fmt.Errorf("enqueue sheet delete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	s.notifyWorker()
	return nil
}

// GetMonthlySummary computes the monthly summary (my_share basis) and daily variable cumulative series.
func (s *Service) GetMonthlySummary(ctx context.Context, year, month int) (MonthlySummaryResponse, error) {
	startDate, endDate, err := monthBounds(year, month)
	if err != nil {
		return MonthlySummaryResponse{}, err
	}

	totals, err := s.queries.GetMonthlyTotals(ctx, db.GetMonthlyTotalsParams{
		StartDate: startDate,
		EndDate:   endDate,
	})
	if err != nil {
		return MonthlySummaryResponse{}, fmt.Errorf("get monthly totals: %w", err)
	}

	outstanding, err := s.queries.GetTotalOutstandingSettlements(ctx)
	if err != nil {
		return MonthlySummaryResponse{}, fmt.Errorf("get outstanding settlements: %w", err)
	}

	dailyRows, err := s.queries.ListDailyVariableCumulative(ctx, db.ListDailyVariableCumulativeParams{
		StartDate: startDate,
		EndDate:   endDate,
	})
	if err != nil {
		return MonthlySummaryResponse{}, fmt.Errorf("list daily variable cumulative: %w", err)
	}

	series := make([]DailyCumulativePoint, len(dailyRows))
	for i, r := range dailyRows {
		series[i] = DailyCumulativePoint{
			Date:       formatDate(r.Date),
			Amount:     r.Amount,
			Cumulative: r.Cumulative,
		}
	}

	return MonthlySummaryResponse{
		Year:                    year,
		Month:                   month,
		FixedTotal:              totals.FixedTotal,
		VariableTotal:           totals.VariableTotal,
		IrregularTotal:          totals.IrregularTotal,
		IncomeTotal:             totals.IncomeTotal,
		PointsSaved:             totals.PointsSaved,
		ReceivableOutstanding:   outstanding.ReceivableOutstanding,
		PayableOutstanding:      outstanding.PayableOutstanding,
		DailyVariableCumulative: series,
	}, nil
}

// ListCategories returns all active categories ordered by sort_order.
func (s *Service) ListCategories(ctx context.Context) ([]CategoryResponse, error) {
	rows, err := s.queries.ListActiveCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	res := make([]CategoryResponse, len(rows))
	for i, r := range rows {
		res[i] = CategoryResponse{
			ID:        r.ID,
			Name:      r.Name,
			SortOrder: r.SortOrder,
		}
	}
	return res, nil
}

// ListPaymentMethods returns all active payment methods ordered by sort_order.
func (s *Service) ListPaymentMethods(ctx context.Context) ([]PaymentMethodResponse, error) {
	rows, err := s.queries.ListActivePaymentMethods(ctx)
	if err != nil {
		return nil, fmt.Errorf("list payment methods: %w", err)
	}
	res := make([]PaymentMethodResponse, len(rows))
	for i, r := range rows {
		res[i] = PaymentMethodResponse{
			ID:        r.ID,
			Name:      r.Name,
			SortOrder: r.SortOrder,
		}
	}
	return res, nil
}

// ListPresets returns all expense presets ordered by sort_order.
func (s *Service) ListPresets(ctx context.Context) ([]PresetResponse, error) {
	rows, err := s.queries.ListExpensePresets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	res := make([]PresetResponse, len(rows))
	for i, r := range rows {
		res[i] = mapPresetResponse(r)
	}
	return res, nil
}

// CreatePreset validates and creates a new expense preset.
func (s *Service) CreatePreset(ctx context.Context, req CreatePresetRequest) (PresetResponse, error) {
	if err := req.Validate(); err != nil {
		return PresetResponse{}, err
	}

	if _, err := s.queries.GetCategoryByID(ctx, req.CategoryID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PresetResponse{}, newValidationError(
				"category_id does not exist",
				httpx.FieldError{Field: "category_id", Message: "unknown category_id"},
			)
		}
		return PresetResponse{}, fmt.Errorf("get category: %w", err)
	}

	var defPM pgtype.Int4
	if req.DefaultPaymentMethodID != nil {
		if _, err := s.queries.GetPaymentMethodByID(ctx, *req.DefaultPaymentMethodID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return PresetResponse{}, newValidationError(
					"default_payment_method_id does not exist",
					httpx.FieldError{Field: "default_payment_method_id", Message: "unknown payment_method_id"},
				)
			}
			return PresetResponse{}, fmt.Errorf("get payment method: %w", err)
		}
		defPM = pgtype.Int4{Int32: *req.DefaultPaymentMethodID, Valid: true}
	}

	var defAmt pgtype.Int4
	if req.DefaultAmount != nil {
		defAmt = pgtype.Int4{Int32: *req.DefaultAmount, Valid: true}
	}

	row, err := s.queries.CreateExpensePreset(ctx, db.CreateExpensePresetParams{
		Title:                  req.Title,
		ExpenseType:            req.ExpenseType,
		CategoryID:             req.CategoryID,
		DefaultAmount:          defAmt,
		DefaultPaymentMethodID: defPM,
		SortOrder:              req.SortOrder,
	})
	if err != nil {
		return PresetResponse{}, fmt.Errorf("create preset: %w", err)
	}
	return mapPresetResponse(row), nil
}

// UpdatePreset updates an existing expense preset.
func (s *Service) UpdatePreset(ctx context.Context, id pgtype.UUID, req UpdatePresetRequest) (PresetResponse, error) {
	current, err := s.queries.GetExpensePresetByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PresetResponse{}, ErrNotFound
		}
		return PresetResponse{}, fmt.Errorf("get preset: %w", err)
	}

	title := current.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	expenseType := current.ExpenseType
	if req.ExpenseType != nil {
		expenseType = strings.TrimSpace(*req.ExpenseType)
	}
	categoryID := current.CategoryID
	if req.CategoryID != nil {
		categoryID = *req.CategoryID
	}
	defAmt := current.DefaultAmount
	if req.ClearDefaultAmount {
		defAmt = pgtype.Int4{}
	} else if req.DefaultAmount != nil {
		defAmt = pgtype.Int4{Int32: *req.DefaultAmount, Valid: true}
	}
	defPM := current.DefaultPaymentMethodID
	if req.ClearDefaultPayment {
		defPM = pgtype.Int4{}
	} else if req.DefaultPaymentMethodID != nil {
		defPM = pgtype.Int4{Int32: *req.DefaultPaymentMethodID, Valid: true}
	}
	sortOrder := current.SortOrder
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	var amtPtr *int32
	if defAmt.Valid {
		v := defAmt.Int32
		amtPtr = &v
	}
	var pmPtr *int32
	if defPM.Valid {
		v := defPM.Int32
		pmPtr = &v
	}

	checkReq := CreatePresetRequest{
		Title:                  title,
		ExpenseType:            expenseType,
		CategoryID:             categoryID,
		DefaultAmount:          amtPtr,
		DefaultPaymentMethodID: pmPtr,
		SortOrder:              sortOrder,
	}
	if err := checkReq.Validate(); err != nil {
		return PresetResponse{}, err
	}

	if req.CategoryID != nil {
		if _, err := s.queries.GetCategoryByID(ctx, categoryID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return PresetResponse{}, newValidationError(
					"category_id does not exist",
					httpx.FieldError{Field: "category_id", Message: "unknown category_id"},
				)
			}
			return PresetResponse{}, fmt.Errorf("get category: %w", err)
		}
	}
	if req.DefaultPaymentMethodID != nil {
		if _, err := s.queries.GetPaymentMethodByID(ctx, *req.DefaultPaymentMethodID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return PresetResponse{}, newValidationError(
					"default_payment_method_id does not exist",
					httpx.FieldError{Field: "default_payment_method_id", Message: "unknown payment_method_id"},
				)
			}
			return PresetResponse{}, fmt.Errorf("get payment method: %w", err)
		}
	}

	updated, err := s.queries.UpdateExpensePreset(ctx, db.UpdateExpensePresetParams{
		ID:                     id,
		Title:                  title,
		ExpenseType:            expenseType,
		CategoryID:             categoryID,
		DefaultAmount:          defAmt,
		DefaultPaymentMethodID: defPM,
		SortOrder:              sortOrder,
	})
	if err != nil {
		return PresetResponse{}, fmt.Errorf("update preset: %w", err)
	}
	return mapPresetResponse(updated), nil
}

// DeletePreset deletes an expense preset by ID.
func (s *Service) DeletePreset(ctx context.Context, id pgtype.UUID) error {
	rows, err := s.queries.DeleteExpensePreset(ctx, id)
	if err != nil {
		return fmt.Errorf("delete preset: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSettlements returns settlements filtered by optional settled status plus per-peer unsettled summary.
func (s *Service) ListSettlements(ctx context.Context, settled *bool) (SettlementsOverviewResponse, error) {
	var settledFilter pgtype.Bool
	if settled != nil {
		settledFilter = pgtype.Bool{Bool: *settled, Valid: true}
	}

	rows, err := s.queries.ListSettlementsWithTransaction(ctx, settledFilter)
	if err != nil {
		return SettlementsOverviewResponse{}, fmt.Errorf("list settlements: %w", err)
	}

	byPeerRows, err := s.queries.ListUnsettledBalancesByPeer(ctx)
	if err != nil {
		return SettlementsOverviewResponse{}, fmt.Errorf("list peer balances: %w", err)
	}

	items := make([]SettlementItemResponse, len(rows))
	for i, r := range rows {
		var memo *string
		if r.Memo.Valid {
			v := r.Memo.String
			memo = &v
		}
		items[i] = SettlementItemResponse{
			ID:            formatUUID(r.ID),
			TransactionID: formatUUID(r.TransactionID),
			TxDate:        formatDate(r.TxDate),
			Title:         r.Title,
			PeerName:      r.PeerName,
			Direction:     r.Direction,
			Amount:        r.Amount,
			Method:        r.Method,
			SettledAt:     r.SettledAt,
			Memo:          memo,
		}
	}

	byPeer := make([]PeerBalanceResponse, len(byPeerRows))
	for i, p := range byPeerRows {
		byPeer[i] = PeerBalanceResponse{
			PeerName:   p.PeerName,
			Receivable: p.Receivable,
			Payable:    p.Payable,
			Net:        p.Net,
		}
	}

	return SettlementsOverviewResponse{
		Items:  items,
		ByPeer: byPeer,
	}, nil
}

// UpdateSettlementStatus toggles the settled_at timestamp on a settlement.
func (s *Service) UpdateSettlementStatus(ctx context.Context, id pgtype.UUID, settled bool) (SettlementItemResponse, error) {
	r, err := s.queries.UpdateSettlementSettledStatus(ctx, db.UpdateSettlementSettledStatusParams{
		ID:      id,
		Settled: settled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SettlementItemResponse{}, ErrNotFound
		}
		return SettlementItemResponse{}, fmt.Errorf("update settlement status: %w", err)
	}

	var memo *string
	if r.Memo.Valid {
		v := r.Memo.String
		memo = &v
	}
	return SettlementItemResponse{
		ID:            formatUUID(r.ID),
		TransactionID: formatUUID(r.TransactionID),
		TxDate:        formatDate(r.TxDate),
		Title:         r.Title,
		PeerName:      r.PeerName,
		Direction:     r.Direction,
		Amount:        r.Amount,
		Method:        r.Method,
		SettledAt:     r.SettledAt,
		Memo:          memo,
	}, nil
}

func (s *Service) buildTransactionResponse(ctx context.Context, q *db.Queries, entry db.VLedgerEntry) (TransactionResponse, error) {
	sts, err := q.ListSettlementsByTransactionID(ctx, entry.ID)
	if err != nil {
		return TransactionResponse{}, fmt.Errorf("list settlements: %w", err)
	}
	stResps := make([]SettlementResponse, len(sts))
	for i, st := range sts {
		stResps[i] = mapSettlementResponse(st)
	}
	return mapLedgerEntryToResponse(entry, stResps), nil
}

func mapLedgerEntryToResponse(entry db.VLedgerEntry, settlements []SettlementResponse) TransactionResponse {
	var expType *string
	if entry.ExpenseType.Valid {
		v := entry.ExpenseType.String
		expType = &v
	}
	var memo *string
	if entry.Memo.Valid {
		v := entry.Memo.String
		memo = &v
	}
	meta := json.RawMessage("{}")
	if len(entry.Metadata) > 0 {
		meta = json.RawMessage(entry.Metadata)
	}
	if settlements == nil {
		settlements = []SettlementResponse{}
	}
	return TransactionResponse{
		ID:              formatUUID(entry.ID),
		TxDate:          formatDate(entry.TxDate),
		Kind:            entry.Kind,
		ExpenseType:     expType,
		CategoryID:      entry.CategoryID,
		Title:           entry.Title,
		GrossAmount:     entry.GrossAmount,
		PointsUsed:      entry.PointsUsed,
		NetAmount:       entry.NetAmount.Int32,
		ReceivableTotal: entry.ReceivableTotal,
		MyShare:         entry.MyShare,
		PaymentMethodID: entry.PaymentMethodID,
		Memo:            memo,
		Metadata:        meta,
		Settlements:     settlements,
		CreatedAt:       entry.CreatedAt,
		UpdatedAt:       entry.UpdatedAt,
	}
}

func mapSettlementResponse(st db.Settlement) SettlementResponse {
	var memo *string
	if st.Memo.Valid {
		v := st.Memo.String
		memo = &v
	}
	return SettlementResponse{
		ID:            formatUUID(st.ID),
		TransactionID: formatUUID(st.TransactionID),
		PeerName:      st.PeerName,
		Direction:     st.Direction,
		Amount:        st.Amount,
		Method:        st.Method,
		SettledAt:     st.SettledAt,
		Memo:          memo,
		CreatedAt:     st.CreatedAt,
		UpdatedAt:     st.UpdatedAt,
	}
}

func mapPresetResponse(p db.ExpensePreset) PresetResponse {
	var defAmt *int32
	if p.DefaultAmount.Valid {
		v := p.DefaultAmount.Int32
		defAmt = &v
	}
	var defPM *int32
	if p.DefaultPaymentMethodID.Valid {
		v := p.DefaultPaymentMethodID.Int32
		defPM = &v
	}
	return PresetResponse{
		ID:                     formatUUID(p.ID),
		Title:                  p.Title,
		ExpenseType:            p.ExpenseType,
		CategoryID:             p.CategoryID,
		DefaultAmount:          defAmt,
		DefaultPaymentMethodID: defPM,
		SortOrder:              p.SortOrder,
		CreatedAt:              p.CreatedAt,
		UpdatedAt:              p.UpdatedAt,
	}
}

func monthBounds(year, month int) (pgtype.Date, pgtype.Date, error) {
	if year < 2000 || year > 2100 {
		return pgtype.Date{}, pgtype.Date{}, newValidationError(
			"year must be between 2000 and 2100",
			httpx.FieldError{Field: "year", Message: "must be between 2000 and 2100"},
		)
	}
	if month < 1 || month > 12 {
		return pgtype.Date{}, pgtype.Date{}, newValidationError(
			"month must be between 1 and 12",
			httpx.FieldError{Field: "month", Message: "must be between 1 and 12"},
		)
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return pgtype.Date{Time: start, Valid: true}, pgtype.Date{Time: end, Valid: true}, nil
}

