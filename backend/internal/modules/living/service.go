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
// For a future month, no rules are due yet unless the user explicitly syncs that month's rules up to its last day?
// Wait: let's check how manual sync should behave when a user clicks "[今月分を即時反映]" or tests sync with a specific year/month.
func (s *Service) SyncRecurringRules(ctx context.Context, year, month int) (SyncRecurringRulesResponse, error) {
	if err := validateYearMonth(year, month, false); err != nil {
		return SyncRecurringRulesResponse{}, err
	}

	nowJST := time.Now().In(s.loc)
	curY, curM, curD := nowJST.Year(), int(nowJST.Month()), nowJST.Day()

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
