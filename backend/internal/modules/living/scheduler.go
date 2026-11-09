package living

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// DaysInMonth returns the last calendar day of the given year and month (e.g., 28/29 for Feb, 30 for Apr, 31 for Jan).
func DaysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// EffectiveDayOfMonth clamps dayOfMonth (1..31) to the last valid calendar day of (year, month).
func EffectiveDayOfMonth(year, month, dayOfMonth int) int {
	if dayOfMonth < 1 {
		return 1
	}
	lastDay := DaysInMonth(year, month)
	if dayOfMonth > lastDay {
		return lastDay
	}
	return dayOfMonth
}

// ComputeNextBillingSchedule calculates the next billing date ("YYYY-MM-DD") and days_until (>= 0)
// relative to todayJST for a recurring rule.
func ComputeNextBillingSchedule(frequency string, dayOfMonth int, monthOfYear *int32, todayJST time.Time) (string, int) {
	todayStart := time.Date(todayJST.Year(), todayJST.Month(), todayJST.Day(), 0, 0, 0, 0, time.UTC)

	var nextDate time.Time
	if frequency == FrequencyYearly && monthOfYear != nil && *monthOfYear >= 1 && *monthOfYear <= 12 {
		targetMonth := int(*monthOfYear)
		y := todayStart.Year()
		effDay := EffectiveDayOfMonth(y, targetMonth, dayOfMonth)
		candidate := time.Date(y, time.Month(targetMonth), effDay, 0, 0, 0, 0, time.UTC)
		if candidate.Before(todayStart) {
			y++
			effDay = EffectiveDayOfMonth(y, targetMonth, dayOfMonth)
			candidate = time.Date(y, time.Month(targetMonth), effDay, 0, 0, 0, 0, time.UTC)
		}
		nextDate = candidate
	} else {
		// Default to MONTHLY
		y := todayStart.Year()
		m := int(todayStart.Month())
		effDay := EffectiveDayOfMonth(y, m, dayOfMonth)
		candidate := time.Date(y, time.Month(m), effDay, 0, 0, 0, 0, time.UTC)
		if candidate.Before(todayStart) {
			if m == 12 {
				y++
				m = 1
			} else {
				m++
			}
			effDay = EffectiveDayOfMonth(y, m, dayOfMonth)
			candidate = time.Date(y, time.Month(m), effDay, 0, 0, 0, 0, time.UTC)
		}
		nextDate = candidate
	}

	daysUntil := int(nextDate.Sub(todayStart).Hours() / 24)
	if daysUntil < 0 {
		daysUntil = 0
	}
	return nextDate.Format("2006-01-02"), daysUntil
}

// DurationUntilNextJSTRun returns the duration until the next daily 00:05:00 JST execution window.
func DurationUntilNextJSTRun(now time.Time, loc *time.Location) time.Duration {
	if loc == nil {
		loc = time.FixedZone("JST", 9*3600)
	}
	nowJST := now.In(loc)
	nextRun := time.Date(nowJST.Year(), nowJST.Month(), nowJST.Day(), 0, 5, 0, 0, loc)
	if !nextRun.After(nowJST) {
		nextRun = nextRun.AddDate(0, 0, 1)
	}
	return nextRun.Sub(nowJST)
}

// CatchUpMonth executes all active auto_create recurring rules due on or before upToDay in (year, month)
// that have not yet been recorded in recurring_executions for "YYYY-MM".
func (s *Service) CatchUpMonth(ctx context.Context, year, month, upToDay int) (SyncRecurringRulesResponse, error) {
	if err := validateYearMonth(year, month, false); err != nil {
		return SyncRecurringRulesResponse{}, err
	}

	lastDay := DaysInMonth(year, month)
	if upToDay < 1 {
		upToDay = 1
	} else if upToDay > lastDay {
		upToDay = lastDay
	}

	targetPeriod := fmt.Sprintf("%04d-%02d", year, month)

	rules, err := s.queries.ListActiveAutoRecurringRules(ctx)
	if err != nil {
		return SyncRecurringRulesResponse{}, fmt.Errorf("list active auto recurring rules: %w", err)
	}

	createdTxIDs := make([]string, 0)
	for _, rule := range rules {
		if !isRuleDueInMonth(rule, year, month, upToDay) {
			continue
		}

		txID, created, err := s.executeSingleRecurringRule(ctx, rule, year, month, targetPeriod)
		if err != nil {
			return SyncRecurringRulesResponse{}, err
		}
		if created {
			createdTxIDs = append(createdTxIDs, txID)
		}
	}

	if len(createdTxIDs) > 0 {
		s.notifyWorker()
	}

	return SyncRecurringRulesResponse{
		Year:           year,
		Month:          month,
		TargetPeriod:   targetPeriod,
		CreatedCount:   len(createdTxIDs),
		TransactionIDs: createdTxIDs,
	}, nil
}

// CatchUpCurrentMonth catches up all due recurring rules for the current JST month up to today's JST day.
func (s *Service) CatchUpCurrentMonth(ctx context.Context, now time.Time) (SyncRecurringRulesResponse, error) {
	nowJST := now.In(s.loc)
	return s.CatchUpMonth(ctx, nowJST.Year(), int(nowJST.Month()), nowJST.Day())
}

func isRuleDueInMonth(rule db.RecurringRule, year, month, upToDay int) bool {
	if !rule.IsActive || !rule.AutoCreate {
		return false
	}

	switch rule.Frequency {
	case FrequencyMonthly:
		// Runs every month
	case FrequencyYearly:
		if !rule.MonthOfYear.Valid || int(rule.MonthOfYear.Int32) != month {
			return false
		}
	default:
		return false
	}

	effDay := EffectiveDayOfMonth(year, month, int(rule.DayOfMonth))
	return effDay <= upToDay
}

func (s *Service) executeSingleRecurringRule(
	ctx context.Context,
	rule db.RecurringRule,
	year, month int,
	targetPeriod string,
) (string, bool, error) {
	// Fast-path check outside transaction to avoid unnecessary write transactions
	if _, err := s.queries.GetRecurringExecutionByRuleAndPeriod(ctx, db.GetRecurringExecutionByRuleAndPeriodParams{
		RuleID:       rule.ID,
		TargetPeriod: targetPeriod,
	}); err == nil {
		return "", false, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, fmt.Errorf("begin recurring rule tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.queries.WithTx(tx)

	effDay := EffectiveDayOfMonth(year, month, int(rule.DayOfMonth))
	txDate := pgtype.Date{
		Time:  time.Date(year, time.Month(month), effDay, 0, 0, 0, 0, time.UTC),
		Valid: true,
	}

	txID, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		TxDate:          txDate,
		Kind:            KindExpense,
		ExpenseType:     pgtype.Text{String: rule.ExpenseType, Valid: true},
		CategoryID:      rule.CategoryID,
		Title:           rule.Title,
		GrossAmount:     rule.Amount,
		PointsUsed:      0,
		PaymentMethodID: rule.PaymentMethodID,
		Memo:            rule.Memo,
		Metadata:        []byte(`{"recurring":true}`),
		Source:          SourceApp,
		SourceRef:       pgtype.Text{},
		IdempotencyKey:  pgtype.UUID{},
	})
	if err != nil {
		return "", false, fmt.Errorf("create transaction for recurring rule %s: %w", formatUUID(rule.ID), err)
	}

	rowsAffected, err := qtx.CreateRecurringExecution(ctx, db.CreateRecurringExecutionParams{
		RuleID:        rule.ID,
		TargetPeriod:  targetPeriod,
		TransactionID: txID,
	})
	if err != nil {
		return "", false, fmt.Errorf("create recurring execution for rule %s: %w", formatUUID(rule.ID), err)
	}
	if rowsAffected == 0 {
		// Another worker/request already executed this rule for targetPeriod; rollback the transaction insert.
		_ = tx.Rollback(ctx)
		return "", false, nil
	}

	if _, err := qtx.EnqueueSheetSync(ctx, db.EnqueueSheetSyncParams{
		TransactionID: txID,
		Op:            OutboxOpUpsert,
	}); err != nil {
		return "", false, fmt.Errorf("enqueue sheet sync for recurring rule %s: %w", formatUUID(rule.ID), err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("commit recurring rule %s: %w", formatUUID(rule.ID), err)
	}

	return formatUUID(txID), true, nil
}

// Scheduler runs the background daily timer that automatically executes due recurring rules in JST.
type Scheduler struct {
	svc    *Service
	logger *slog.Logger
	loc    *time.Location
}

// NewScheduler creates a new recurring rules background Scheduler.
func NewScheduler(svc *Service, logger *slog.Logger, loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.FixedZone("JST", 9*3600)
	}
	return &Scheduler{
		svc:    svc,
		logger: logger,
		loc:    loc,
	}
}

// Run executes an immediate catch-up on startup and then schedules daily executions at 00:05:00 JST.
func (sch *Scheduler) Run(ctx context.Context) {
	sch.runCatchUp(ctx, "startup")

	for {
		waitDur := DurationUntilNextJSTRun(time.Now(), sch.loc)
		timer := time.NewTimer(waitDur)

		select {
		case <-ctx.Done():
			timer.Stop()
			sch.logger.Info("recurring_scheduler_stopped")
			return
		case <-timer.C:
			sch.runCatchUp(ctx, "daily_timer")
		}
	}
}

func (sch *Scheduler) runCatchUp(ctx context.Context, trigger string) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	res, err := sch.svc.CatchUpCurrentMonth(runCtx, time.Now())
	if err != nil {
		sch.logger.ErrorContext(runCtx, "recurring_scheduler_catchup_failed",
			slog.String("trigger", trigger),
			slog.String("error", err.Error()),
		)
		return
	}

	if res.CreatedCount > 0 {
		sch.logger.InfoContext(runCtx, "recurring_scheduler_executed",
			slog.String("trigger", trigger),
			slog.String("target_period", res.TargetPeriod),
			slog.Int("created_count", res.CreatedCount),
		)
	}
}

