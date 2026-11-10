package living

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service implements domain logic and persistence for the Living module.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	loc     *time.Location
	wakeCh  chan<- struct{}
}

// NewService creates a new Living Service.
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

// GetBudgetOverview returns the default budget (0, 0), the monthly override for (year, month),
// and the effective budget applied for that month.
func (s *Service) GetBudgetOverview(ctx context.Context, year, month int) (BudgetOverviewResponse, error) {
	if err := validateYearMonth(year, month, false); err != nil {
		return BudgetOverviewResponse{}, err
	}

	var defaultItem *BudgetItemResponse
	if defRow, err := s.queries.GetBudgetByYearMonth(ctx, db.GetBudgetByYearMonthParams{
		Year:  0,
		Month: 0,
	}); err == nil {
		mapped := mapBudgetRow(defRow)
		defaultItem = &mapped
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BudgetOverviewResponse{}, fmt.Errorf("get default budget: %w", err)
	}

	var overrideItem *BudgetItemResponse
	if ovRow, err := s.queries.GetBudgetByYearMonth(ctx, db.GetBudgetByYearMonthParams{
		Year:  int32(year),
		Month: int32(month),
	}); err == nil {
		mapped := mapBudgetRow(ovRow)
		overrideItem = &mapped
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return BudgetOverviewResponse{}, fmt.Errorf("get monthly budget override: %w", err)
	}

	var effective *int32
	if overrideItem != nil {
		v := overrideItem.VariableBudget
		effective = &v
	} else if defaultItem != nil {
		v := defaultItem.VariableBudget
		effective = &v
	}

	return BudgetOverviewResponse{
		Year:            year,
		Month:           month,
		DefaultBudget:   defaultItem,
		MonthlyOverride: overrideItem,
		EffectiveBudget: effective,
	}, nil
}

// UpsertBudget creates or updates the default budget (year=0, month=0) or a monthly budget override.
func (s *Service) UpsertBudget(ctx context.Context, req UpsertBudgetRequest) (BudgetItemResponse, error) {
	if err := req.Validate(); err != nil {
		return BudgetItemResponse{}, err
	}

	var memo pgtype.Text
	if req.Memo != nil && strings.TrimSpace(*req.Memo) != "" {
		memo = pgtype.Text{String: strings.TrimSpace(*req.Memo), Valid: true}
	}

	row, err := s.queries.UpsertBudget(ctx, db.UpsertBudgetParams{
		Year:           int32(req.Year),
		Month:          int32(req.Month),
		VariableBudget: *req.VariableBudget,
		Memo:           memo,
	})
	if err != nil {
		return BudgetItemResponse{}, fmt.Errorf("upsert budget: %w", err)
	}

	return mapBudgetRow(row), nil
}

// DeleteBudget deletes a budget entry for (year, month) — either a monthly override or the default budget (0, 0).
func (s *Service) DeleteBudget(ctx context.Context, year, month int) error {
	if err := validateYearMonth(year, month, true); err != nil {
		return err
	}

	rows, err := s.queries.DeleteBudget(ctx, db.DeleteBudgetParams{
		Year:  int32(year),
		Month: int32(month),
	})
	if err != nil {
		return fmt.Errorf("delete budget: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRecurringRules returns all recurring rules annotated with execution status for (year, month)
// and upcoming billing schedule relative to today in JST.
func (s *Service) ListRecurringRules(ctx context.Context, year, month int) (RecurringRulesOverviewResponse, error) {
	if err := validateYearMonth(year, month, false); err != nil {
		return RecurringRulesOverviewResponse{}, err
	}

	rules, err := s.queries.ListRecurringRules(ctx)
	if err != nil {
		return RecurringRulesOverviewResponse{}, fmt.Errorf("list recurring rules: %w", err)
	}

	targetPeriod := fmt.Sprintf("%04d-%02d", year, month)
	executions, err := s.queries.ListRecurringExecutionsByPeriod(ctx, targetPeriod)
	if err != nil {
		return RecurringRulesOverviewResponse{}, fmt.Errorf("list recurring executions: %w", err)
	}

	execMap := make(map[pgtype.UUID]pgtype.UUID, len(executions))
	for _, ex := range executions {
		execMap[ex.RuleID] = ex.TransactionID
	}

	nowJST := time.Now().In(s.loc)
	items := make([]RecurringRuleResponse, len(rules))
	for i, r := range rules {
		var txID *string
		executed := false
		if tid, ok := execMap[r.ID]; ok && tid.Valid {
			executed = true
			formatted := formatUUID(tid)
			txID = &formatted
		}
		items[i] = mapRecurringRuleRow(r, executed, txID, nowJST)
	}

	return RecurringRulesOverviewResponse{
		Year:  year,
		Month: month,
		Items: items,
	}, nil
}

// CreateRecurringRule validates and creates a new recurring fixed expense or subscription rule.
func (s *Service) CreateRecurringRule(ctx context.Context, req CreateRecurringRuleRequest) (RecurringRuleResponse, error) {
	expenseType := strings.TrimSpace(req.ExpenseType)
	if expenseType == "" {
		expenseType = ExpenseTypeFixed
	}

	frequency := strings.TrimSpace(req.Frequency)
	if frequency == "" {
		frequency = FrequencyMonthly
	}

	dayOfMonth := req.DayOfMonth
	if dayOfMonth == 0 {
		dayOfMonth = 1
	}

	autoCreate := true
	if req.AutoCreate != nil {
		autoCreate = *req.AutoCreate
	}

	isSubscription := false
	if req.IsSubscription != nil {
		isSubscription = *req.IsSubscription
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	isAdvance, err := s.validateCategoryAndPaymentMethod(ctx, req.CategoryID, req.PaymentMethodID)
	if err != nil {
		return RecurringRuleResponse{}, err
	}

	if err := ValidateRecurringRuleFields(
		req.Title,
		expenseType,
		req.CategoryID,
		req.Amount,
		req.PaymentMethodID,
		isAdvance,
		frequency,
		dayOfMonth,
		req.MonthOfYear,
		req.Memo,
	); err != nil {
		return RecurringRuleResponse{}, err
	}

	var monthOfYear pgtype.Int4
	if frequency == FrequencyYearly && req.MonthOfYear != nil {
		monthOfYear = pgtype.Int4{Int32: *req.MonthOfYear, Valid: true}
	}

	var memo pgtype.Text
	if req.Memo != nil && strings.TrimSpace(*req.Memo) != "" {
		memo = pgtype.Text{String: strings.TrimSpace(*req.Memo), Valid: true}
	}

	row, err := s.queries.CreateRecurringRule(ctx, db.CreateRecurringRuleParams{
		Title:           strings.TrimSpace(req.Title),
		ExpenseType:     expenseType,
		CategoryID:      req.CategoryID,
		Amount:          req.Amount,
		PaymentMethodID: req.PaymentMethodID,
		Frequency:       frequency,
		DayOfMonth:      dayOfMonth,
		MonthOfYear:     monthOfYear,
		AutoCreate:      autoCreate,
		IsSubscription:  isSubscription,
		Memo:            memo,
		SortOrder:       req.SortOrder,
		IsActive:        isActive,
	})
	if err != nil {
		return RecurringRuleResponse{}, fmt.Errorf("create recurring rule: %w", err)
	}

	nowJST := time.Now().In(s.loc)
	return mapRecurringRuleRow(row, false, nil, nowJST), nil
}

// UpdateRecurringRule partially updates an existing recurring rule.
func (s *Service) UpdateRecurringRule(ctx context.Context, id pgtype.UUID, req UpdateRecurringRuleRequest) (RecurringRuleResponse, error) {
	current, err := s.queries.GetRecurringRuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecurringRuleResponse{}, ErrNotFound
		}
		return RecurringRuleResponse{}, fmt.Errorf("get recurring rule: %w", err)
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

	amount := current.Amount
	if req.Amount != nil {
		amount = *req.Amount
	}

	paymentMethodID := current.PaymentMethodID
	if req.PaymentMethodID != nil {
		paymentMethodID = *req.PaymentMethodID
	}

	frequency := current.Frequency
	if req.Frequency != nil {
		frequency = strings.TrimSpace(*req.Frequency)
	}

	dayOfMonth := current.DayOfMonth
	if req.DayOfMonth != nil {
		dayOfMonth = *req.DayOfMonth
	}

	var monthOfYearPtr *int32
	if current.MonthOfYear.Valid {
		v := current.MonthOfYear.Int32
		monthOfYearPtr = &v
	}
	if req.MonthOfYear != nil {
		if *req.MonthOfYear == 0 {
			monthOfYearPtr = nil
		} else {
			v := *req.MonthOfYear
			monthOfYearPtr = &v
		}
	}
	if frequency == FrequencyMonthly {
		monthOfYearPtr = nil
	}

	autoCreate := current.AutoCreate
	if req.AutoCreate != nil {
		autoCreate = *req.AutoCreate
	}

	isSubscription := current.IsSubscription
	if req.IsSubscription != nil {
		isSubscription = *req.IsSubscription
	}

	var memoPtr *string
	if current.Memo.Valid {
		v := current.Memo.String
		memoPtr = &v
	}
	if req.Memo != nil {
		trimmed := strings.TrimSpace(*req.Memo)
		memoPtr = &trimmed
	}

	sortOrder := current.SortOrder
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	isActive := current.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	isAdvance, err := s.validateCategoryAndPaymentMethod(ctx, categoryID, paymentMethodID)
	if err != nil {
		return RecurringRuleResponse{}, err
	}

	if err := ValidateRecurringRuleFields(
		title,
		expenseType,
		categoryID,
		amount,
		paymentMethodID,
		isAdvance,
		frequency,
		dayOfMonth,
		monthOfYearPtr,
		memoPtr,
	); err != nil {
		return RecurringRuleResponse{}, err
	}

	var monthOfYear pgtype.Int4
	if frequency == FrequencyYearly && monthOfYearPtr != nil {
		monthOfYear = pgtype.Int4{Int32: *monthOfYearPtr, Valid: true}
	}

	var memo pgtype.Text
	if memoPtr != nil && strings.TrimSpace(*memoPtr) != "" {
		memo = pgtype.Text{String: strings.TrimSpace(*memoPtr), Valid: true}
	}

	updated, err := s.queries.UpdateRecurringRule(ctx, db.UpdateRecurringRuleParams{
		ID:              id,
		Title:           strings.TrimSpace(title),
		ExpenseType:     expenseType,
		CategoryID:      categoryID,
		Amount:          amount,
		PaymentMethodID: paymentMethodID,
		Frequency:       frequency,
		DayOfMonth:      dayOfMonth,
		MonthOfYear:     monthOfYear,
		AutoCreate:      autoCreate,
		IsSubscription:  isSubscription,
		Memo:            memo,
		SortOrder:       sortOrder,
		IsActive:        isActive,
	})
	if err != nil {
		return RecurringRuleResponse{}, fmt.Errorf("update recurring rule: %w", err)
	}

	nowJST := time.Now().In(s.loc)
	targetPeriod := fmt.Sprintf("%04d-%02d", nowJST.Year(), int(nowJST.Month()))
	var txID *string
	executed := false
	if ex, err := s.queries.GetRecurringExecutionByRuleAndPeriod(ctx, db.GetRecurringExecutionByRuleAndPeriodParams{
		RuleID:       id,
		TargetPeriod: targetPeriod,
	}); err == nil && ex.TransactionID.Valid {
		executed = true
		formatted := formatUUID(ex.TransactionID)
		txID = &formatted
	}

	return mapRecurringRuleRow(updated, executed, txID, nowJST), nil
}

// DeleteRecurringRule deletes a recurring rule by UUID.
func (s *Service) DeleteRecurringRule(ctx context.Context, id pgtype.UUID) error {
	rows, err := s.queries.DeleteRecurringRule(ctx, id)
	if err != nil {
		return fmt.Errorf("delete recurring rule: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// SyncRecurringRules triggers manual catch-up execution for the given (year, month).
// For the current JST month, rules with effectiveDay <= today's day are executed.
// For a past month, all rules due in that month (up to the last day of that month) are executed.
// For a future month after the current JST month, no rules are due yet.
func (s *Service) SyncRecurringRules(ctx context.Context, year, month int) (SyncRecurringRulesResponse, error) {
	if err := validateYearMonth(year, month, false); err != nil {
		return SyncRecurringRulesResponse{}, err
	}

	nowJST := time.Now().In(s.loc)
	curY, curM, curD := nowJST.Year(), int(nowJST.Month()), nowJST.Day()

	if year > curY || (year == curY && month > curM) {
		return SyncRecurringRulesResponse{
			Year:           year,
			Month:          month,
			TargetPeriod:   fmt.Sprintf("%04d-%02d", year, month),
			CreatedCount:   0,
			TransactionIDs: []string{},
		}, nil
	}

	var upToDay int
	if year == curY && month == curM {
		upToDay = curD
	} else {
		upToDay = DaysInMonth(year, month)
	}

	return s.CatchUpMonth(ctx, year, month, upToDay)
}

func (s *Service) validateCategoryAndPaymentMethod(ctx context.Context, categoryID, paymentMethodID int32) (bool, error) {
	if categoryID > 0 {
		cat, err := s.queries.GetCategoryByID(ctx, categoryID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, newValidationError(
					"category_id does not exist",
					httpx.FieldError{Field: "category_id", Message: "unknown category_id"},
				)
			}
			return false, fmt.Errorf("get category: %w", err)
		}
		if !cat.IsActive {
			return false, newValidationError(
				"category is inactive",
				httpx.FieldError{Field: "category_id", Message: "category is inactive"},
			)
		}
	}

	var isAdvance bool
	if paymentMethodID > 0 {
		pm, err := s.queries.GetPaymentMethodByID(ctx, paymentMethodID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, newValidationError(
					"payment_method_id does not exist",
					httpx.FieldError{Field: "payment_method_id", Message: "unknown payment_method_id"},
				)
			}
			return false, fmt.Errorf("get payment method: %w", err)
		}
		if !pm.IsActive {
			return false, newValidationError(
				"payment method is inactive",
				httpx.FieldError{Field: "payment_method_id", Message: "payment method is inactive"},
			)
		}
		isAdvance = pm.Name == PaymentMethodAdvance
	}

	return isAdvance, nil
}

func validateYearMonth(year, month int, allowDefault bool) error {
	if allowDefault && year == 0 && month == 0 {
		return nil
	}
	var fields []httpx.FieldError
	if year < 2020 || year > 2100 {
		fields = append(fields, httpx.FieldError{
			Field:   "year",
			Message: "must be between 2020 and 2100",
		})
	}
	if month < 1 || month > 12 {
		fields = append(fields, httpx.FieldError{
			Field:   "month",
			Message: "must be between 1 and 12",
		})
	}
	if len(fields) > 0 {
		return newValidationError(fields[0].Message, fields...)
	}
	return nil
}

func mapBudgetRow(r db.MonthlyBudget) BudgetItemResponse {
	var memo *string
	if r.Memo.Valid {
		v := r.Memo.String
		memo = &v
	}
	return BudgetItemResponse{
		Year:           int(r.Year),
		Month:          int(r.Month),
		VariableBudget: r.VariableBudget,
		Memo:           memo,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func mapRecurringRuleRow(r db.RecurringRule, executedInMonth bool, txID *string, todayJST time.Time) RecurringRuleResponse {
	var monthOfYear *int32
	if r.MonthOfYear.Valid {
		v := r.MonthOfYear.Int32
		monthOfYear = &v
	}
	var memo *string
	if r.Memo.Valid {
		v := r.Memo.String
		memo = &v
	}

	nextBillingDate, daysUntil := ComputeNextBillingSchedule(r.Frequency, int(r.DayOfMonth), monthOfYear, todayJST)

	return RecurringRuleResponse{
		ID:              formatUUID(r.ID),
		Title:           r.Title,
		ExpenseType:     r.ExpenseType,
		CategoryID:      r.CategoryID,
		Amount:          r.Amount,
		PaymentMethodID: r.PaymentMethodID,
		Frequency:       r.Frequency,
		DayOfMonth:      r.DayOfMonth,
		MonthOfYear:     monthOfYear,
		AutoCreate:      r.AutoCreate,
		IsSubscription:  r.IsSubscription,
		Memo:            memo,
		SortOrder:       r.SortOrder,
		IsActive:        r.IsActive,
		ExecutedInMonth: executedInMonth,
		TransactionID:   txID,
		NextBillingDate: nextBillingDate,
		DaysUntil:       daysUntil,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// GetFurusatoOverview returns the tax-year profile, simplified deduction limit, totals, and donation list.
func (s *Service) GetFurusatoOverview(ctx context.Context, taxYear int) (FurusatoOverviewResponse, error) {
	if taxYear < 2020 || taxYear > 2100 {
		return FurusatoOverviewResponse{}, newValidationError(
			"year must be between 2020 and 2100",
			httpx.FieldError{Field: "year", Message: "must be between 2020 and 2100"},
		)
	}

	profile := FurusatoProfileResponse{
		TaxYear:         taxYear,
		GrossIncome:     0,
		SocialInsurance: 0,
		IdecoAnnual:     0,
		FamilyStatus:    FamilyStatusSingle,
	}

	if row, err := s.queries.GetFurusatoProfileByYear(ctx, int32(taxYear)); err == nil {
		updated := row.UpdatedAt
		profile = FurusatoProfileResponse{
			TaxYear:         int(row.TaxYear),
			GrossIncome:     row.GrossIncome,
			SocialInsurance: row.SocialInsurance,
			IdecoAnnual:     row.IdecoAnnual,
			FamilyStatus:    row.FamilyStatus,
			UpdatedAt:       &updated,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return FurusatoOverviewResponse{}, fmt.Errorf("get furusato profile: %w", err)
	}

	donations, err := s.queries.ListFurusatoDonationsByYear(ctx, int32(taxYear))
	if err != nil {
		return FurusatoOverviewResponse{}, fmt.Errorf("list furusato donations: %w", err)
	}

	return buildFurusatoOverview(taxYear, profile, donations), nil
}

// UpsertFurusatoProfile saves or updates the user's tax-year profile and returns the refreshed FurusatoOverviewResponse.
func (s *Service) UpsertFurusatoProfile(ctx context.Context, taxYear int, req UpsertFurusatoProfileRequest) (FurusatoOverviewResponse, error) {
	if err := req.Validate(taxYear); err != nil {
		return FurusatoOverviewResponse{}, err
	}

	var socialInsurance int32
	if req.SocialInsurance != nil {
		socialInsurance = *req.SocialInsurance
	}

	var idecoAnnual int32
	if req.IdecoAnnual != nil {
		idecoAnnual = *req.IdecoAnnual
	}

	familyStatus := FamilyStatusSingle
	if req.FamilyStatus != nil && *req.FamilyStatus != "" {
		familyStatus = *req.FamilyStatus
	}

	row, err := s.queries.UpsertFurusatoProfile(ctx, db.UpsertFurusatoProfileParams{
		TaxYear:         int32(taxYear),
		GrossIncome:     *req.GrossIncome,
		SocialInsurance: socialInsurance,
		IdecoAnnual:     idecoAnnual,
		FamilyStatus:    familyStatus,
	})
	if err != nil {
		return FurusatoOverviewResponse{}, fmt.Errorf("upsert furusato profile: %w", err)
	}

	updated := row.UpdatedAt
	profile := FurusatoProfileResponse{
		TaxYear:         int(row.TaxYear),
		GrossIncome:     row.GrossIncome,
		SocialInsurance: row.SocialInsurance,
		IdecoAnnual:     row.IdecoAnnual,
		FamilyStatus:    row.FamilyStatus,
		UpdatedAt:       &updated,
	}

	donations, err := s.queries.ListFurusatoDonationsByYear(ctx, int32(taxYear))
	if err != nil {
		return FurusatoOverviewResponse{}, fmt.Errorf("list furusato donations: %w", err)
	}

	return buildFurusatoOverview(taxYear, profile, donations), nil
}

// CreateFurusatoDonation registers a new Furusato Nozei donation.
// When req.CreateExpense is true, a PUBLIC_FEES expense transaction and sheet_sync_outbox entry are created atomically.
func (s *Service) CreateFurusatoDonation(ctx context.Context, req CreateFurusatoDonationRequest) (FurusatoDonationResponse, error) {
	nowJST := time.Now().In(s.loc)

	donatedAtStr := strings.TrimSpace(req.DonatedAt)
	if donatedAtStr == "" {
		donatedAtStr = nowJST.Format("2006-01-02")
	}

	parsedDonatedAtPre, _ := time.Parse("2006-01-02", donatedAtStr)
	taxYear := nowJST.Year()
	if !parsedDonatedAtPre.IsZero() {
		taxYear = parsedDonatedAtPre.Year()
	}
	if req.TaxYear != nil {
		taxYear = *req.TaxYear
	}

	onestopMethod := strings.TrimSpace(req.OnestopMethod)
	if onestopMethod == "" {
		onestopMethod = OnestopMethodOnlineIAM
	}

	onestopCompleted := false
	if req.OnestopCompleted != nil {
		onestopCompleted = *req.OnestopCompleted
	}

	deadlineStr := fmt.Sprintf("%04d-01-10", taxYear+1)
	if req.ApplicationDeadline != nil && strings.TrimSpace(*req.ApplicationDeadline) != "" {
		deadlineStr = strings.TrimSpace(*req.ApplicationDeadline)
	}

	parsedDonatedAt, parsedDeadline, err := ValidateFurusatoDonationFields(
		taxYear,
		donatedAtStr,
		req.Municipality,
		req.ReturnGift,
		req.Amount,
		onestopMethod,
		deadlineStr,
		req.Memo,
	)
	if err != nil {
		return FurusatoDonationResponse{}, err
	}

	var completedAt *time.Time
	if onestopCompleted {
		now := time.Now()
		completedAt = &now
	}

	var memo pgtype.Text
	if req.Memo != nil && strings.TrimSpace(*req.Memo) != "" {
		memo = pgtype.Text{String: strings.TrimSpace(*req.Memo), Valid: true}
	}

	trimmedMunicipality := strings.TrimSpace(req.Municipality)
	trimmedGift := strings.TrimSpace(req.ReturnGift)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("begin furusato donation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	var linkedTxID pgtype.UUID
	if req.CreateExpense {
		pubFeesCat, err := qtx.GetCategoryByName(ctx, "PUBLIC_FEES")
		if err != nil {
			return FurusatoDonationResponse{}, fmt.Errorf("resolve PUBLIC_FEES category: %w", err)
		}

		paymentMethodID, err := s.resolveFurusatoPaymentMethod(ctx, qtx, req.PaymentMethodID)
		if err != nil {
			return FurusatoDonationResponse{}, err
		}

		txTitle := formatFurusatoExpenseTitle(trimmedMunicipality, trimmedGift)

		createdTxID, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
			TxDate:          pgtype.Date{Time: parsedDonatedAt, Valid: true},
			Kind:            KindExpense,
			ExpenseType:     pgtype.Text{String: ExpenseTypeVariable, Valid: true},
			CategoryID:      pubFeesCat.ID,
			Title:           txTitle,
			GrossAmount:     req.Amount,
			PointsUsed:      0,
			PaymentMethodID: paymentMethodID,
			Memo:            memo,
			Metadata:        []byte(`{"furusato":true}`),
			Source:          SourceApp,
			SourceRef:       pgtype.Text{},
			IdempotencyKey:  pgtype.UUID{},
		})
		if err != nil {
			return FurusatoDonationResponse{}, fmt.Errorf("create furusato expense transaction: %w", err)
		}

		if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
			TransactionID: createdTxID,
			Op:            OutboxOpUpsert,
		}); err != nil {
			return FurusatoDonationResponse{}, fmt.Errorf("enqueue sheet sync for furusato donation: %w", err)
		}

		linkedTxID = createdTxID
	}

	row, err := qtx.CreateFurusatoDonation(ctx, db.CreateFurusatoDonationParams{
		TaxYear:             int32(taxYear),
		DonatedAt:           pgtype.Date{Time: parsedDonatedAt, Valid: true},
		Municipality:        trimmedMunicipality,
		ReturnGift:          trimmedGift,
		Amount:              req.Amount,
		OnestopMethod:       onestopMethod,
		OnestopCompleted:    onestopCompleted,
		CompletedAt:         completedAt,
		ApplicationDeadline: pgtype.Date{Time: parsedDeadline, Valid: true},
		TransactionID:       linkedTxID,
		Memo:                memo,
	})
	if err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("create furusato donation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("commit furusato donation: %w", err)
	}

	if req.CreateExpense {
		s.notifyWorker()
	}

	return mapFurusatoDonationRow(row), nil
}

// UpdateFurusatoDonation updates a Furusato Nozei donation or toggles onestop_completed.
func (s *Service) UpdateFurusatoDonation(ctx context.Context, id pgtype.UUID, req UpdateFurusatoDonationRequest) (FurusatoDonationResponse, error) {
	current, err := s.queries.GetFurusatoDonationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FurusatoDonationResponse{}, ErrNotFound
		}
		return FurusatoDonationResponse{}, fmt.Errorf("get furusato donation: %w", err)
	}

	taxYear := int(current.TaxYear)
	if req.TaxYear != nil {
		taxYear = *req.TaxYear
	}

	donatedAtStr := current.DonatedAt.Time.Format("2006-01-02")
	if req.DonatedAt != nil {
		donatedAtStr = strings.TrimSpace(*req.DonatedAt)
	}

	municipality := current.Municipality
	if req.Municipality != nil {
		municipality = strings.TrimSpace(*req.Municipality)
	}

	returnGift := current.ReturnGift
	if req.ReturnGift != nil {
		returnGift = strings.TrimSpace(*req.ReturnGift)
	}

	amount := current.Amount
	if req.Amount != nil {
		amount = *req.Amount
	}

	onestopMethod := current.OnestopMethod
	if req.OnestopMethod != nil {
		onestopMethod = strings.TrimSpace(*req.OnestopMethod)
	}

	onestopCompleted := current.OnestopCompleted
	completedAt := current.CompletedAt
	if req.OnestopCompleted != nil {
		onestopCompleted = *req.OnestopCompleted
		if onestopCompleted && !current.OnestopCompleted {
			now := time.Now()
			completedAt = &now
		} else if !onestopCompleted {
			completedAt = nil
		}
	}

	deadlineStr := current.ApplicationDeadline.Time.Format("2006-01-02")
	if req.ApplicationDeadline != nil {
		deadlineStr = strings.TrimSpace(*req.ApplicationDeadline)
	}

	var memoPtr *string
	if current.Memo.Valid {
		v := current.Memo.String
		memoPtr = &v
	}
	if req.Memo != nil {
		trimmed := strings.TrimSpace(*req.Memo)
		memoPtr = &trimmed
	}

	parsedDonatedAt, parsedDeadline, err := ValidateFurusatoDonationFields(
		taxYear,
		donatedAtStr,
		municipality,
		returnGift,
		amount,
		onestopMethod,
		deadlineStr,
		memoPtr,
	)
	if err != nil {
		return FurusatoDonationResponse{}, err
	}

	var memo pgtype.Text
	if memoPtr != nil && strings.TrimSpace(*memoPtr) != "" {
		memo = pgtype.Text{String: strings.TrimSpace(*memoPtr), Valid: true}
	}

	trimmedMunicipality := strings.TrimSpace(municipality)
	trimmedGift := strings.TrimSpace(returnGift)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("begin update furusato donation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	updated, err := qtx.UpdateFurusatoDonation(ctx, db.UpdateFurusatoDonationParams{
		ID:                  id,
		TaxYear:             int32(taxYear),
		DonatedAt:           pgtype.Date{Time: parsedDonatedAt, Valid: true},
		Municipality:        trimmedMunicipality,
		ReturnGift:          trimmedGift,
		Amount:              amount,
		OnestopMethod:       onestopMethod,
		OnestopCompleted:    onestopCompleted,
		CompletedAt:         completedAt,
		ApplicationDeadline: pgtype.Date{Time: parsedDeadline, Valid: true},
		Memo:                memo,
	})
	if err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("update furusato donation: %w", err)
	}

	ledgerFieldsChanged := req.DonatedAt != nil ||
		req.Municipality != nil ||
		req.ReturnGift != nil ||
		req.Amount != nil ||
		req.Memo != nil

	syncedLedgerTx := false
	if updated.TransactionID.Valid && ledgerFieldsChanged {
		existingEntry, err := qtx.GetLedgerEntryByID(ctx, updated.TransactionID)
		if err == nil {
			txTitle := formatFurusatoExpenseTitle(trimmedMunicipality, trimmedGift)
			grossAmount := amount
			if existingEntry.PointsUsed > grossAmount {
				grossAmount = existingEntry.PointsUsed
			}
			if _, err := qtx.UpdateTransaction(ctx, db.UpdateTransactionParams{
				ID:              updated.TransactionID,
				TxDate:          pgtype.Date{Time: parsedDonatedAt, Valid: true},
				Kind:            existingEntry.Kind,
				ExpenseType:     existingEntry.ExpenseType,
				CategoryID:      existingEntry.CategoryID,
				Title:           txTitle,
				GrossAmount:     grossAmount,
				PointsUsed:      existingEntry.PointsUsed,
				PaymentMethodID: existingEntry.PaymentMethodID,
				Memo:            memo,
				Metadata:        existingEntry.Metadata,
			}); err != nil {
				return FurusatoDonationResponse{}, fmt.Errorf("update linked furusato expense transaction: %w", err)
			}
			if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
				TransactionID: updated.TransactionID,
				Op:            OutboxOpUpsert,
			}); err != nil {
				return FurusatoDonationResponse{}, fmt.Errorf("enqueue sheet sync for updated furusato donation: %w", err)
			}
			syncedLedgerTx = true
		} else if errors.Is(err, pgx.ErrNoRows) {
			if err := qtx.ClearFurusatoDonationTransactionLink(ctx, updated.TransactionID); err != nil {
				return FurusatoDonationResponse{}, fmt.Errorf("clear stale furusato transaction link: %w", err)
			}
			updated.TransactionID = pgtype.UUID{}
		} else {
			return FurusatoDonationResponse{}, fmt.Errorf("load linked furusato transaction: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return FurusatoDonationResponse{}, fmt.Errorf("commit update furusato donation: %w", err)
	}

	if syncedLedgerTx {
		s.notifyWorker()
	}

	return mapFurusatoDonationRow(updated), nil
}

// DeleteFurusatoDonation deletes a Furusato Nozei donation by UUID and soft-deletes any linked expense transaction.
func (s *Service) DeleteFurusatoDonation(ctx context.Context, id pgtype.UUID) error {
	current, err := s.queries.GetFurusatoDonationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get furusato donation: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin delete furusato donation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	rows, err := qtx.DeleteFurusatoDonation(ctx, id)
	if err != nil {
		return fmt.Errorf("delete furusato donation: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	deletedLinkedTx := false
	if current.TransactionID.Valid {
		softRows, err := qtx.SoftDeleteTransaction(ctx, current.TransactionID)
		if err != nil {
			return fmt.Errorf("soft delete linked furusato transaction: %w", err)
		}
		if softRows > 0 {
			if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
				TransactionID: current.TransactionID,
				Op:            OutboxOpDelete,
			}); err != nil {
				return fmt.Errorf("enqueue sheet delete for linked furusato transaction: %w", err)
			}
			deletedLinkedTx = true
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete furusato donation: %w", err)
	}

	if deletedLinkedTx {
		s.notifyWorker()
	}

	return nil
}

func formatFurusatoExpenseTitle(municipality, returnGift string) string {
	txTitle := fmt.Sprintf("ふるさと納税 (%s)", municipality)
	if returnGift != "" {
		candidate := fmt.Sprintf("ふるさと納税 (%s・%s)", municipality, returnGift)
		if len([]rune(candidate)) <= 200 {
			txTitle = candidate
		}
	}
	return txTitle
}

func (s *Service) resolveFurusatoPaymentMethod(ctx context.Context, qtx *db.Queries, requestedID *int32) (int32, error) {
	if requestedID != nil && *requestedID > 0 {
		pm, err := qtx.GetPaymentMethodByID(ctx, *requestedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, newValidationError(
					"payment_method_id does not exist",
					httpx.FieldError{Field: "payment_method_id", Message: "unknown payment_method_id"},
				)
			}
			return 0, fmt.Errorf("get payment method: %w", err)
		}
		if !pm.IsActive {
			return 0, newValidationError(
				"payment method is inactive",
				httpx.FieldError{Field: "payment_method_id", Message: "payment method is inactive"},
			)
		}
		if pm.Name == PaymentMethodAdvance {
			return 0, newValidationError(
				"ADVANCE payment method cannot be used for furusato donations",
				httpx.FieldError{Field: "payment_method_id", Message: "ADVANCE cannot be used"},
			)
		}
		return pm.ID, nil
	}

	// Default to CREDIT_CARD when not specified
	if pm, err := qtx.GetPaymentMethodByName(ctx, "CREDIT_CARD"); err == nil {
		return pm.ID, nil
	}
	pms, err := qtx.ListActivePaymentMethods(ctx)
	if err != nil || len(pms) == 0 {
		return 1, nil
	}
	return pms[0].ID, nil
}

func buildFurusatoOverview(taxYear int, profile FurusatoProfileResponse, rows []db.FurusatoDonation) FurusatoOverviewResponse {
	breakdown := EstimateFurusatoLimit(
		profile.GrossIncome,
		profile.SocialInsurance,
		profile.IdecoAnnual,
		profile.FamilyStatus,
	)

	items := make([]FurusatoDonationResponse, len(rows))
	var donatedTotal int32
	uncompletedCount := 0

	for i, r := range rows {
		items[i] = mapFurusatoDonationRow(r)
		donatedTotal += r.Amount
		if !r.OnestopCompleted {
			uncompletedCount++
		}
	}

	remaining := breakdown.EstimatedLimit - donatedTotal

	return FurusatoOverviewResponse{
		TaxYear:                 taxYear,
		FormulaLabel:            FurusatoFormulaLabel,
		FormulaUpdatedAt:        FurusatoFormulaUpdatedAt,
		FormulaCaption:          FurusatoFormulaCaption,
		Profile:                 profile,
		EstimatedLimit:          breakdown.EstimatedLimit,
		CalculationBreakdown:    breakdown,
		DonatedTotal:            donatedTotal,
		RemainingLimit:          remaining,
		UncompletedOnestopCount: uncompletedCount,
		Items:                   items,
	}
}

func mapFurusatoDonationRow(r db.FurusatoDonation) FurusatoDonationResponse {
	var txID *string
	if r.TransactionID.Valid {
		formatted := formatUUID(r.TransactionID)
		txID = &formatted
	}
	var memo *string
	if r.Memo.Valid {
		v := r.Memo.String
		memo = &v
	}

	return FurusatoDonationResponse{
		ID:                  formatUUID(r.ID),
		TaxYear:             int(r.TaxYear),
		DonatedAt:           r.DonatedAt.Time.Format("2006-01-02"),
		Municipality:        r.Municipality,
		ReturnGift:          r.ReturnGift,
		Amount:              r.Amount,
		OnestopMethod:       r.OnestopMethod,
		OnestopCompleted:    r.OnestopCompleted,
		CompletedAt:         r.CompletedAt,
		ApplicationDeadline: r.ApplicationDeadline.Time.Format("2006-01-02"),
		TransactionID:       txID,
		Memo:                memo,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}


