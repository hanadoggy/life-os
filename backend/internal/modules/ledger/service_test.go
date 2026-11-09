package ledger

import (
	"encoding/json"
	"errors"
	"testing"
)

func strPtr(s string) *string {
	return &s
}

func TestValidateTransactionAccounting(t *testing.T) {
	tests := []struct {
		name             string
		title            string
		kind             string
		expenseType      *string
		categoryID       int32
		paymentMethodID  int32
		isAdvancePayment bool
		grossAmount      int32
		pointsUsed       int32
		metadata         json.RawMessage
		settlements      []SettlementInput
		wantErr          bool
		wantErrField     string
	}{
		{
			name:            "valid standard expense with points and RECEIVABLE settlements",
			title:           "라멘회 (멘야 무사시)",
			kind:            KindExpense,
			expenseType:     strPtr(ExpenseTypeVariable),
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     3600,
			pointsUsed:      600, // net = 3000
			settlements: []SettlementInput{
				{PeerName: "에이키", Direction: DirectionReceivable, Amount: 1200, Method: "PayPay"},
				{PeerName: "테츠", Direction: DirectionReceivable, Amount: 1200, Method: "PayPay"},
			}, // receivable_total = 2400 <= 3000 (my_share = 600)
			wantErr: false,
		},
		{
			name:             "valid ADVANCE (立替) expense with matching PAYABLE settlement",
			title:            "점심 대납",
			kind:             KindExpense,
			expenseType:      strPtr(ExpenseTypeVariable),
			categoryID:       1,
			paymentMethodID:  5,
			isAdvancePayment: true,
			grossAmount:      1500,
			pointsUsed:       0,
			settlements: []SettlementInput{
				{PeerName: "테츠", Direction: DirectionPayable, Amount: 1500, Method: "PayPay"},
			},
			wantErr: false,
		},
		{
			name:            "valid INCOME transaction",
			title:           "급여",
			kind:            KindIncome,
			expenseType:     nil,
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     300000,
			pointsUsed:      0,
			wantErr:         false,
		},
		{
			name:            "rejects points_used > gross_amount",
			title:           "올림픽 마트",
			kind:            KindExpense,
			expenseType:     strPtr(ExpenseTypeVariable),
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     1000,
			pointsUsed:      1200,
			wantErr:         true,
			wantErrField:    "points_used",
		},
		{
			name:            "rejects RECEIVABLE total exceeding net_amount",
			title:           "저녁 모임",
			kind:            KindExpense,
			expenseType:     strPtr(ExpenseTypeVariable),
			categoryID:      2,
			paymentMethodID: 1,
			grossAmount:     3000,
			pointsUsed:      1000, // net = 2000
			settlements: []SettlementInput{
				{PeerName: "에이키", Direction: DirectionReceivable, Amount: 1200},
				{PeerName: "테츠", Direction: DirectionReceivable, Amount: 1200}, // 2400 > 2000
			},
			wantErr:      true,
			wantErrField: "settlements",
		},
		{
			name:             "rejects ADVANCE payment method without PAYABLE settlement",
			title:            "대납 누락",
			kind:             KindExpense,
			expenseType:      strPtr(ExpenseTypeVariable),
			categoryID:       1,
			paymentMethodID:  5,
			isAdvancePayment: true,
			grossAmount:      1500,
			pointsUsed:       0,
			settlements:      nil,
			wantErr:          true,
			wantErrField:     "settlements",
		},
		{
			name:             "rejects ADVANCE payment method when PAYABLE amount != net_amount",
			title:            "대납 금액 불일치",
			kind:             KindExpense,
			expenseType:      strPtr(ExpenseTypeVariable),
			categoryID:       1,
			paymentMethodID:  5,
			isAdvancePayment: true,
			grossAmount:      1500,
			pointsUsed:       0,
			settlements: []SettlementInput{
				{PeerName: "테츠", Direction: DirectionPayable, Amount: 1000},
			},
			wantErr:      true,
			wantErrField: "settlements",
		},
		{
			name:             "rejects PAYABLE settlement when payment method is not ADVANCE",
			title:            "일반 카드인데 PAYABLE 입력",
			kind:             KindExpense,
			expenseType:      strPtr(ExpenseTypeVariable),
			categoryID:       1,
			paymentMethodID:  1,
			isAdvancePayment: false,
			grossAmount:      1500,
			pointsUsed:       0,
			settlements: []SettlementInput{
				{PeerName: "테츠", Direction: DirectionPayable, Amount: 1500},
			},
			wantErr:      true,
			wantErrField: "payment_method_id",
		},
		{
			name:            "rejects INCOME with non-nil expense_type",
			title:           "보너스",
			kind:            KindIncome,
			expenseType:     strPtr(ExpenseTypeIrregular),
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     100000,
			pointsUsed:      0,
			wantErr:         true,
			wantErrField:    "expense_type",
		},
		{
			name:            "rejects INCOME with non-zero points_used",
			title:           "보너스",
			kind:            KindIncome,
			expenseType:     nil,
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     100000,
			pointsUsed:      500,
			wantErr:         true,
			wantErrField:    "points_used",
		},
		{
			name:            "rejects INCOME with settlements",
			title:           "급여",
			kind:            KindIncome,
			expenseType:     nil,
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     300000,
			pointsUsed:      0,
			settlements: []SettlementInput{
				{PeerName: "에이키", Direction: DirectionReceivable, Amount: 1000},
			},
			wantErr:      true,
			wantErrField: "settlements",
		},
		{
			name:            "rejects gross_amount exceeding MaxYenAmount",
			title:           "오입력",
			kind:            KindExpense,
			expenseType:     strPtr(ExpenseTypeVariable),
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     MaxYenAmount + 1,
			pointsUsed:      0,
			wantErr:         true,
			wantErrField:    "gross_amount",
		},
		{
			name:            "prevents int32 overflow when summing RECEIVABLE settlements",
			title:           "대형 정산 합산",
			kind:            KindExpense,
			expenseType:     strPtr(ExpenseTypeVariable),
			categoryID:      1,
			paymentMethodID: 1,
			grossAmount:     MaxYenAmount,
			pointsUsed:      0,
			settlements: []SettlementInput{
				{PeerName: "에이키", Direction: DirectionReceivable, Amount: 60_000_000},
				{PeerName: "테츠", Direction: DirectionReceivable, Amount: 60_000_000},
			},
			wantErr:      true,
			wantErrField: "settlements",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTransactionAccounting(
				tc.title,
				tc.kind,
				tc.expenseType,
				tc.categoryID,
				tc.paymentMethodID,
				tc.isAdvancePayment,
				tc.grossAmount,
				tc.pointsUsed,
				tc.metadata,
				tc.settlements,
			)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var valErr *ValidationError
				if !errors.As(err, &valErr) {
					t.Fatalf("expected *ValidationError, got %T: %v", err, err)
				}
				if tc.wantErrField != "" {
					found := false
					for _, fe := range valErr.Fields {
						if fe.Field == tc.wantErrField {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected field error on %q, got fields: %+v", tc.wantErrField, valErr.Fields)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

