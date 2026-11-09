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

