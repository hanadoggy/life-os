package living

import (
	"strings"
	"testing"
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

