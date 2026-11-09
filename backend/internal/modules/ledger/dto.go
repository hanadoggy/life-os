package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	KindExpense = "EXPENSE"
	KindIncome  = "INCOME"

	ExpenseTypeFixed     = "FIXED"
	ExpenseTypeVariable  = "VARIABLE"
	ExpenseTypeIrregular = "IRREGULAR"

	DirectionReceivable = "RECEIVABLE"
	DirectionPayable    = "PAYABLE"

	PaymentMethodAdvance = "ADVANCE"
	DefaultSettleMethod  = "PayPay"
	SourceApp            = "APP"

	OutboxOpUpsert = "UPSERT"
	OutboxOpDelete = "DELETE"

	// MaxYenAmount caps single transaction/settlement amounts at 100,000,000 JPY to prevent overflow.
	MaxYenAmount int32 = 100_000_000
)

var (
	// ErrNotFound indicates the requested resource does not exist or is soft-deleted.
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

// ConflictError represents a 409 business state conflict (e.g., modifying settled transactions).
type ConflictError struct {
	Detail string
}

func (e *ConflictError) Error() string {
	return e.Detail
}

// SettlementInput represents a settlement item inside a transaction create/update request.
type SettlementInput struct {
	PeerName  string  `json:"peer_name"`
	Direction string  `json:"direction"`
	Amount    int32   `json:"amount"`
	Method    string  `json:"method"`
	Memo      *string `json:"memo,omitempty"`
}

// CreateTransactionRequest is the request payload for POST /api/v1/transactions.
type CreateTransactionRequest struct {
	TxDate          string            `json:"tx_date"`
	Kind            string            `json:"kind"`
	ExpenseType     *string           `json:"expense_type"`
	CategoryID      int32             `json:"category_id"`
	Title           string            `json:"title"`
	GrossAmount     int32             `json:"gross_amount"`
	PointsUsed      int32             `json:"points_used"`
	PaymentMethodID int32             `json:"payment_method_id"`
	Memo            *string           `json:"memo"`
	Metadata        json.RawMessage   `json:"metadata"`
	Settlements     []SettlementInput `json:"settlements"`
}

// UpdateTransactionRequest is the request payload for PATCH /api/v1/transactions/{id}.
type UpdateTransactionRequest struct {
	TxDate          *string            `json:"tx_date"`
	Kind            *string            `json:"kind"`
	ExpenseType     *string            `json:"expense_type"`
	CategoryID      *int32             `json:"category_id"`
	Title           *string            `json:"title"`
	GrossAmount     *int32             `json:"gross_amount"`
	PointsUsed      *int32             `json:"points_used"`
	PaymentMethodID *int32             `json:"payment_method_id"`
	Memo            *string            `json:"memo"`
	Metadata        *json.RawMessage   `json:"metadata"`
	Settlements     *[]SettlementInput `json:"settlements"`
}

// SettlementResponse represents a settlement item in API responses.
type SettlementResponse struct {
	ID            string     `json:"id"`
	TransactionID string     `json:"transaction_id"`
	PeerName      string     `json:"peer_name"`
	Direction     string     `json:"direction"`
	Amount        int32      `json:"amount"`
	Method        string     `json:"method"`
	SettledAt     *time.Time `json:"settled_at"`
	Memo          *string    `json:"memo,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TransactionResponse represents a ledger entry with computed amounts and settlements.
type TransactionResponse struct {
	ID              string               `json:"id"`
	TxDate          string               `json:"tx_date"`
	Kind            string               `json:"kind"`
	ExpenseType     *string              `json:"expense_type"`
	CategoryID      int32                `json:"category_id"`
	Title           string               `json:"title"`
	GrossAmount     int32                `json:"gross_amount"`
	PointsUsed      int32                `json:"points_used"`
	NetAmount       int32                `json:"net_amount"`
	ReceivableTotal int32                `json:"receivable_total"`
	MyShare         int32                `json:"my_share"`
	PaymentMethodID int32                `json:"payment_method_id"`
	Memo            *string              `json:"memo,omitempty"`
	Metadata        json.RawMessage      `json:"metadata"`
	Settlements     []SettlementResponse `json:"settlements"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

// DailyCumulativePoint represents a single day's variable expense and running cumulative total.
type DailyCumulativePoint struct {
	Date       string `json:"date"`
	Amount     int32  `json:"amount"`
	Cumulative int32  `json:"cumulative"`
}

// MonthlySummaryResponse is the response for GET /api/v1/ledger/summary.
type MonthlySummaryResponse struct {
	Year                    int                    `json:"year"`
	Month                   int                    `json:"month"`
	FixedTotal              int32                  `json:"fixed_total"`
	VariableTotal           int32                  `json:"variable_total"`
	IrregularTotal          int32                  `json:"irregular_total"`
	IncomeTotal             int32                  `json:"income_total"`
	PointsSaved             int32                  `json:"points_saved"`
	ReceivableOutstanding   int32                  `json:"receivable_outstanding"`
	PayableOutstanding      int32                  `json:"payable_outstanding"`
	DailyVariableCumulative []DailyCumulativePoint `json:"daily_variable_cumulative"`
}

// CategoryResponse represents a category item.
type CategoryResponse struct {
	ID        int32  `json:"id"`
	Name      string `json:"name"`
	SortOrder int32  `json:"sort_order"`
}

// PaymentMethodResponse represents a payment method item.
type PaymentMethodResponse struct {
	ID        int32  `json:"id"`
	Name      string `json:"name"`
	SortOrder int32  `json:"sort_order"`
}

// PresetResponse represents an expense preset item.
type PresetResponse struct {
	ID                     string    `json:"id"`
	Title                  string    `json:"title"`
	ExpenseType            string    `json:"expense_type"`
	CategoryID             int32     `json:"category_id"`
	DefaultAmount          *int32    `json:"default_amount"`
	DefaultPaymentMethodID *int32    `json:"default_payment_method_id"`
	SortOrder              int32     `json:"sort_order"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// CreatePresetRequest is the request payload for POST /api/v1/presets.
type CreatePresetRequest struct {
	Title                  string `json:"title"`
	ExpenseType            string `json:"expense_type"`
	CategoryID             int32  `json:"category_id"`
	DefaultAmount          *int32 `json:"default_amount"`
	DefaultPaymentMethodID *int32 `json:"default_payment_method_id"`
	SortOrder              int32  `json:"sort_order"`
}

// Validate checks CreatePresetRequest constraints.
func (r *CreatePresetRequest) Validate() error {
	r.Title = strings.TrimSpace(r.Title)
	var fields []httpx.FieldError

	if l := utf8.RuneCountInString(r.Title); l < 1 || l > 100 {
		fields = append(fields, httpx.FieldError{Field: "title", Message: "must be between 1 and 100 characters"})
	}
	if !isValidExpenseType(r.ExpenseType) {
		fields = append(fields, httpx.FieldError{Field: "expense_type", Message: "must be FIXED, VARIABLE, or IRREGULAR"})
	}
	if r.CategoryID <= 0 {
		fields = append(fields, httpx.FieldError{Field: "category_id", Message: "must be a positive integer"})
	}
	if r.DefaultAmount != nil && *r.DefaultAmount < 0 {
		fields = append(fields, httpx.FieldError{Field: "default_amount", Message: "must be >= 0"})
	}
	if r.DefaultPaymentMethodID != nil && *r.DefaultPaymentMethodID <= 0 {
		fields = append(fields, httpx.FieldError{Field: "default_payment_method_id", Message: "must be a positive integer"})
	}
	if len(fields) > 0 {
		return newValidationError(fields[0].Message, fields...)
	}
	return nil
}

// UpdatePresetRequest is the request payload for PATCH /api/v1/presets/{id}.
type UpdatePresetRequest struct {
	Title                  *string `json:"title"`
	ExpenseType            *string `json:"expense_type"`
	CategoryID             *int32  `json:"category_id"`
	DefaultAmount          *int32  `json:"default_amount"`
	ClearDefaultAmount     bool    `json:"clear_default_amount"`
	DefaultPaymentMethodID *int32  `json:"default_payment_method_id"`
	ClearDefaultPayment    bool    `json:"clear_default_payment_method"`
	SortOrder              *int32  `json:"sort_order"`
}

// SettlementItemResponse represents a settlement item joined with its parent transaction info.
type SettlementItemResponse struct {
	ID            string     `json:"id"`
	TransactionID string     `json:"transaction_id"`
	TxDate        string     `json:"tx_date"`
	Title         string     `json:"title"`
	PeerName      string     `json:"peer_name"`
	Direction     string     `json:"direction"`
	Amount        int32      `json:"amount"`
	Method        string     `json:"method"`
	SettledAt     *time.Time `json:"settled_at"`
	Memo          *string    `json:"memo,omitempty"`
}

// PeerBalanceResponse represents per-peer unsettled amounts.
type PeerBalanceResponse struct {
	PeerName   string `json:"peer_name"`
	Receivable int32  `json:"receivable"`
	Payable    int32  `json:"payable"`
	Net        int32  `json:"net"`
}

// SettlementsOverviewResponse is the response for GET /api/v1/settlements.
type SettlementsOverviewResponse struct {
	Items  []SettlementItemResponse `json:"items"`
	ByPeer []PeerBalanceResponse    `json:"by_peer"`
}

// UpdateSettlementStatusRequest is the request payload for PATCH /api/v1/settlements/{id}.
type UpdateSettlementStatusRequest struct {
	Settled *bool `json:"settled"`
}

func isValidExpenseType(v string) bool {
	switch v {
	case ExpenseTypeFixed, ExpenseTypeVariable, ExpenseTypeIrregular:
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

func formatDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func parseDate(raw string) (pgtype.Date, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

