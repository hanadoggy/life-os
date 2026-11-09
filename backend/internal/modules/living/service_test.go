package living

import (
	"strings"
	"testing"
	"time"
)

func int32Ptr(v int32) *int32 {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func TestUpsertBudgetRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     UpsertBudgetRequest
		wantErr bool
	}{
		{
			name: "valid default budget (0, 0)",
			req: UpsertBudgetRequest{
				Year:           0,
				Month:          0,
				VariableBudget: int32Ptr(80000),
				Memo:           strPtr("毎月デフォルト予算"),
			},
			wantErr: false,
		},
		{
			name: "valid monthly override (2026, 11)",
			req: UpsertBudgetRequest{
				Year:           2026,
				Month:          11,
				VariableBudget: int32Ptr(95000),
			},
			wantErr: false,
		},
		{
			name: "zero variable budget is allowed",
			req: UpsertBudgetRequest{
				Year:           2026,
				Month:          11,
				VariableBudget: int32Ptr(0),
			},
			wantErr: false,
		},
		{
			name: "invalid partial zero (year=0, month=11)",
			req: UpsertBudgetRequest{
				Year:           0,
				Month:          11,
				VariableBudget: int32Ptr(80000),
			},
			wantErr: true,
		},
		{
			name: "invalid partial zero (year=2026, month=0)",
			req: UpsertBudgetRequest{
				Year:           2026,
				Month:          0,
				VariableBudget: int32Ptr(80000),
			},
			wantErr: true,
		},
		{
			name: "missing variable_budget",
			req: UpsertBudgetRequest{
				Year:  2026,
				Month: 11,
			},
			wantErr: true,
		},
		{
			name: "negative variable_budget",
			req: UpsertBudgetRequest{
				Year:           2026,
				Month:          11,
				VariableBudget: int32Ptr(-100),
			},
			wantErr: true,
		},
		{
			name: "memo too long",
			req: UpsertBudgetRequest{
				Year:           2026,
				Month:          11,
				VariableBudget: int32Ptr(80000),
				Memo:           strPtr(strings.Repeat("あ", 201)),
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEffectiveDayOfMonth(t *testing.T) {
	tests := []struct {
		year       int
		month      int
		dayOfMonth int
		want       int
	}{
		{2026, 2, 31, 28},  // Non-leap February clamps 31 -> 28
		{2028, 2, 31, 29},  // Leap year February clamps 31 -> 29
		{2026, 4, 31, 30},  // April clamps 31 -> 30
		{2026, 11, 31, 30}, // November clamps 31 -> 30
		{2026, 12, 31, 31}, // December keeps 31
		{2026, 11, 25, 25}, // Normal day 25 stays 25
	}

	for _, tc := range tests {
		got := EffectiveDayOfMonth(tc.year, tc.month, tc.dayOfMonth)
		if got != tc.want {
			t.Errorf("EffectiveDayOfMonth(%d, %d, %d) = %d, want %d", tc.year, tc.month, tc.dayOfMonth, got, tc.want)
		}
	}
}

func TestComputeNextBillingSchedule(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	// Reference date: 2026-11-10
	today := time.Date(2026, 11, 10, 14, 30, 0, 0, jst)

	tests := []struct {
		name        string
		frequency   string
		dayOfMonth  int
		monthOfYear *int32
		wantDate    string
		wantDays    int
	}{
		{
			name:       "monthly due today (D-0)",
			frequency:  FrequencyMonthly,
			dayOfMonth: 10,
			wantDate:   "2026-11-10",
			wantDays:   0,
		},
		{
			name:       "monthly due later this month",
			frequency:  FrequencyMonthly,
			dayOfMonth: 25,
			wantDate:   "2026-11-25",
			wantDays:   15,
		},
		{
			name:       "monthly end-of-month clamping in November (31 -> Nov 30)",
			frequency:  FrequencyMonthly,
			dayOfMonth: 31,
			wantDate:   "2026-11-30",
			wantDays:   20,
		},
		{
			name:       "monthly already passed this month -> rolls to December",
			frequency:  FrequencyMonthly,
			dayOfMonth: 5,
			wantDate:   "2026-12-05",
			wantDays:   25,
		},
		{
			name:        "yearly due next month in same year",
			frequency:   FrequencyYearly,
			dayOfMonth:  15,
			monthOfYear: int32Ptr(12),
			wantDate:    "2026-12-15",
			wantDays:    35,
		},
		{
			name:        "yearly already passed in current year -> rolls to next year",
			frequency:   FrequencyYearly,
			dayOfMonth:  20,
			monthOfYear: int32Ptr(4),
			wantDate:    "2027-04-20",
			wantDays:    161,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDate, gotDays := ComputeNextBillingSchedule(tc.frequency, tc.dayOfMonth, tc.monthOfYear, today)
			if gotDate != tc.wantDate || gotDays != tc.wantDays {
				t.Fatalf("ComputeNextBillingSchedule() = (%s, %d), want (%s, %d)", gotDate, gotDays, tc.wantDate, tc.wantDays)
			}
		})
	}
}

func TestValidateRecurringRuleFields(t *testing.T) {
	tests := []struct {
		name            string
		title           string
		expenseType     string
		categoryID      int32
		amount          int32
		paymentMethodID int32
		isAdvance       bool
		frequency       string
		dayOfMonth      int32
		monthOfYear     *int32
		wantErr         bool
	}{
		{
			name:            "valid monthly fixed rule",
			title:           "家賃",
			expenseType:     ExpenseTypeFixed,
			categoryID:      6,
			amount:          98000,
			paymentMethodID: 1,
			isAdvance:       false,
			frequency:       FrequencyMonthly,
			dayOfMonth:      27,
			monthOfYear:     nil,
			wantErr:         false,
		},
		{
			name:            "valid yearly subscription rule",
			title:           "Amazon Prime 年会費",
			expenseType:     ExpenseTypeFixed,
			categoryID:      10,
			amount:          5900,
			paymentMethodID: 1,
			isAdvance:       false,
			frequency:       FrequencyYearly,
			dayOfMonth:      15,
			monthOfYear:     int32Ptr(4),
			wantErr:         false,
		},
		{
			name:            "reject monthly with month_of_year set",
			title:           "Netflix",
			expenseType:     ExpenseTypeFixed,
			categoryID:      10,
			amount:          1590,
			paymentMethodID: 1,
			isAdvance:       false,
			frequency:       FrequencyMonthly,
			dayOfMonth:      10,
			monthOfYear:     int32Ptr(5),
			wantErr:         true,
		},
		{
			name:            "reject yearly without month_of_year",
			title:           "AppleCare+",
			expenseType:     ExpenseTypeFixed,
			categoryID:      10,
			amount:          14800,
			paymentMethodID: 1,
			isAdvance:       false,
			frequency:       FrequencyYearly,
			dayOfMonth:      1,
			monthOfYear:     nil,
			wantErr:         true,
		},
		{
			name:            "reject ADVANCE payment method",
			title:           "シェアハウス立替",
			expenseType:     ExpenseTypeFixed,
			categoryID:      6,
			amount:          50000,
			paymentMethodID: 5,
			isAdvance:       true,
			frequency:       FrequencyMonthly,
			dayOfMonth:      1,
			monthOfYear:     nil,
			wantErr:         true,
		},
		{
			name:            "reject day_of_month out of range",
			title:           "光回線",
			expenseType:     ExpenseTypeFixed,
			categoryID:      8,
			amount:          4950,
			paymentMethodID: 1,
			isAdvance:       false,
			frequency:       FrequencyMonthly,
			dayOfMonth:      32,
			monthOfYear:     nil,
			wantErr:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRecurringRuleFields(
				tc.title,
				tc.expenseType,
				tc.categoryID,
				tc.amount,
				tc.paymentMethodID,
				tc.isAdvance,
				tc.frequency,
				tc.dayOfMonth,
				tc.monthOfYear,
				nil,
			)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateRecurringRuleFields() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}


