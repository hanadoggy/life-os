package living

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// MaxYenAmount caps single budget/rule amounts at 100,000,000 JPY to prevent overflow.
	MaxYenAmount int32 = 100_000_000

	KindExpense = "EXPENSE"

	ExpenseTypeFixed    = "FIXED"
	ExpenseTypeVariable = "VARIABLE"

	FrequencyMonthly = "MONTHLY"
	FrequencyYearly  = "YEARLY"

	PaymentMethodAdvance = "ADVANCE"
	SourceApp            = "APP"
	OutboxOpUpsert       = "UPSERT"
)

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
)

// ValidationError represents a 422 domain/input validation failure.
type ValidationError struct {
	Detail string
	Fields []httpx.FieldError
}

func (e *ValidationError) Error() string {
	return e.Detail
}

func newValidationError(detail string, fields ...httpx.FieldError) *ValidationError {
	return &ValidationError{
		Detail: detail,
		Fields: fields,
	}
}

// BudgetItemResponse represents a single monthly_budgets row in API responses.
type BudgetItemResponse struct {
	Year           int       `json:"year"`
	Month          int       `json:"month"`
	VariableBudget int32     `json:"variable_budget"`
	Memo           *string   `json:"memo,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BudgetOverviewResponse is the response for GET /api/v1/living/budgets.
type BudgetOverviewResponse struct {
	Year            int                 `json:"year"`
	Month           int                 `json:"month"`
	DefaultBudget   *BudgetItemResponse `json:"default_budget"`
	MonthlyOverride *BudgetItemResponse `json:"monthly_override"`
	EffectiveBudget *int32              `json:"effective_budget"`
}

// UpsertBudgetRequest is the request payload for PUT /api/v1/living/budgets.
type UpsertBudgetRequest struct {
	Year           int     `json:"year"`
	Month          int     `json:"month"`
	VariableBudget *int32  `json:"variable_budget"`
	Memo           *string `json:"memo"`
}

// Validate checks UpsertBudgetRequest constraints.
func (r *UpsertBudgetRequest) Validate() error {
	var fields []httpx.FieldError

	isDefault := r.Year == 0 && r.Month == 0
	isMonthly := r.Year >= 2020 && r.Year <= 2100 && r.Month >= 1 && r.Month <= 12

	if !isDefault && !isMonthly {
		fields = append(fields, httpx.FieldError{
			Field:   "year_month",
			Message: "must be (year=0, month=0) for default budget or (year between 2020..2100, month between 1..12)",
		})
	}

	if r.VariableBudget == nil {
		fields = append(fields, httpx.FieldError{
			Field:   "variable_budget",
			Message: "is required",
		})
	} else if *r.VariableBudget < 0 || *r.VariableBudget > MaxYenAmount {
		fields = append(fields, httpx.FieldError{
			Field:   "variable_budget",
			Message: fmt.Sprintf("must be between 0 and %d", MaxYenAmount),
		})
	}

	if r.Memo != nil {
		trimmed := strings.TrimSpace(*r.Memo)
		r.Memo = &trimmed
		if utf8.RuneCountInString(trimmed) > 200 {
			fields = append(fields, httpx.FieldError{
				Field:   "memo",
				Message: "must be at most 200 characters",
			})
		}
	}

	if len(fields) > 0 {
		return newValidationError(fields[0].Message, fields...)
	}
	return nil
}

// RecurringRuleResponse represents a recurring fixed expense or subscription rule with execution status and next billing schedule.
type RecurringRuleResponse struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	ExpenseType     string    `json:"expense_type"`
	CategoryID      int32     `json:"category_id"`
	Amount          int32     `json:"amount"`
	PaymentMethodID int32     `json:"payment_method_id"`
	Frequency       string    `json:"frequency"`
	DayOfMonth      int32     `json:"day_of_month"`
	MonthOfYear     *int32    `json:"month_of_year"`
	AutoCreate      bool      `json:"auto_create"`
	IsSubscription  bool      `json:"is_subscription"`
	Memo            *string   `json:"memo,omitempty"`
	SortOrder       int32     `json:"sort_order"`
	IsActive        bool      `json:"is_active"`
	ExecutedInMonth bool      `json:"executed_in_month"`
	TransactionID   *string   `json:"transaction_id,omitempty"`
	NextBillingDate string    `json:"next_billing_date"`
	DaysUntil       int       `json:"days_until"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// RecurringRulesOverviewResponse is the response for GET /api/v1/living/recurring-rules.
type RecurringRulesOverviewResponse struct {
	Year  int                     `json:"year"`
	Month int                     `json:"month"`
	Items []RecurringRuleResponse `json:"items"`
}

// CreateRecurringRuleRequest is the request payload for POST /api/v1/living/recurring-rules.
type CreateRecurringRuleRequest struct {
	Title           string  `json:"title"`
	ExpenseType     string  `json:"expense_type"`
	CategoryID      int32   `json:"category_id"`
	Amount          int32   `json:"amount"`
	PaymentMethodID int32   `json:"payment_method_id"`
	Frequency       string  `json:"frequency"`
	DayOfMonth      int32   `json:"day_of_month"`
	MonthOfYear     *int32  `json:"month_of_year"`
	AutoCreate      *bool   `json:"auto_create"`
	IsSubscription  *bool   `json:"is_subscription"`
	Memo            *string `json:"memo"`
	SortOrder       int32   `json:"sort_order"`
	IsActive        *bool   `json:"is_active"`
}

// UpdateRecurringRuleRequest is the request payload for PATCH /api/v1/living/recurring-rules/{id}.
type UpdateRecurringRuleRequest struct {
	Title           *string `json:"title"`
	ExpenseType     *string `json:"expense_type"`
	CategoryID      *int32  `json:"category_id"`
	Amount          *int32  `json:"amount"`
	PaymentMethodID *int32  `json:"payment_method_id"`
	Frequency       *string `json:"frequency"`
	DayOfMonth      *int32  `json:"day_of_month"`
	MonthOfYear     *int32  `json:"month_of_year"`
	AutoCreate      *bool   `json:"auto_create"`
	IsSubscription  *bool   `json:"is_subscription"`
	Memo            *string `json:"memo"`
	SortOrder       *int32  `json:"sort_order"`
	IsActive        *bool   `json:"is_active"`
}

// SyncRecurringRulesRequest is the optional request payload for POST /api/v1/living/recurring-rules/sync.
type SyncRecurringRulesRequest struct {
	Year  *int `json:"year"`
	Month *int `json:"month"`
}

// SyncRecurringRulesResponse is the response for POST /api/v1/living/recurring-rules/sync.
type SyncRecurringRulesResponse struct {
	Year           int      `json:"year"`
	Month          int      `json:"month"`
	TargetPeriod   string   `json:"target_period"`
	CreatedCount   int      `json:"created_count"`
	TransactionIDs []string `json:"transaction_ids"`
}

// ValidateRecurringRuleFields validates domain constraints for a recurring rule.
// This function is pure so it can be unit-tested without a database connection.
func ValidateRecurringRuleFields(
	title string,
	expenseType string,
	categoryID int32,
	amount int32,
	paymentMethodID int32,
	isAdvancePayment bool,
	frequency string,
	dayOfMonth int32,
	monthOfYear *int32,
	memo *string,
) error {
	var fields []httpx.FieldError

	trimmedTitle := strings.TrimSpace(title)
	if l := utf8.RuneCountInString(trimmedTitle); l < 1 || l > 100 {
		fields = append(fields, httpx.FieldError{
			Field:   "title",
			Message: "must be between 1 and 100 characters",
		})
	}

	if !isValidExpenseType(expenseType) {
		fields = append(fields, httpx.FieldError{
			Field:   "expense_type",
			Message: "must be FIXED or VARIABLE",
		})
	}

	if categoryID <= 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "category_id",
			Message: "must be a positive integer",
		})
	}

	if amount < 0 || amount > MaxYenAmount {
		fields = append(fields, httpx.FieldError{
			Field:   "amount",
			Message: fmt.Sprintf("must be between 0 and %d", MaxYenAmount),
		})
	}

	if paymentMethodID <= 0 {
		fields = append(fields, httpx.FieldError{
			Field:   "payment_method_id",
			Message: "must be a positive integer",
		})
	} else if isAdvancePayment {
		fields = append(fields, httpx.FieldError{
			Field:   "payment_method_id",
			Message: "ADVANCE (立替) payment method cannot be used for recurring rules",
		})
	}

	switch frequency {
	case FrequencyMonthly:
		if monthOfYear != nil {
			fields = append(fields, httpx.FieldError{
				Field:   "month_of_year",
				Message: "must be null when frequency is MONTHLY",
			})
		}
	case FrequencyYearly:
		if monthOfYear == nil || *monthOfYear < 1 || *monthOfYear > 12 {
			fields = append(fields, httpx.FieldError{
				Field:   "month_of_year",
				Message: "must be between 1 and 12 when frequency is YEARLY",
			})
		}
	default:
		fields = append(fields, httpx.FieldError{
			Field:   "frequency",
			Message: "must be MONTHLY or YEARLY",
		})
	}

	if dayOfMonth < 1 || dayOfMonth > 31 {
		fields = append(fields, httpx.FieldError{
			Field:   "day_of_month",
			Message: "must be between 1 and 31",
		})
	}

	if memo != nil {
		trimmed := strings.TrimSpace(*memo)
		if utf8.RuneCountInString(trimmed) > 200 {
			fields = append(fields, httpx.FieldError{
				Field:   "memo",
				Message: "must be at most 200 characters",
			})
		}
	}

	if len(fields) > 0 {
		return newValidationError(fields[0].Message, fields...)
	}
	return nil
}

func isValidExpenseType(v string) bool {
	switch v {
	case ExpenseTypeFixed, ExpenseTypeVariable:
		return true
	default:
		return false
	}
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseUUID(raw string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(strings.TrimSpace(raw))
	if err != nil || !u.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID %q", raw)
	}
	return u, nil
}
