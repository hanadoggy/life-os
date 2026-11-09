package living

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
)

const (
	// MaxYenAmount caps single budget/rule amounts at 100,000,000 JPY to prevent overflow.
	MaxYenAmount int32 = 100_000_000
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

