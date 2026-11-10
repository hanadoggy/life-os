package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// MaxBodyBytes limits JSON request bodies to 64 KiB (Specification §6.0).
	MaxBodyBytes = 64 << 10
	// ContentTypeProblemJSON is the RFC 9457 media type for Problem Details.
	ContentTypeProblemJSON = "application/problem+json; charset=utf-8"
	// ContentTypeJSON is the standard JSON media type.
	ContentTypeJSON = "application/json; charset=utf-8"
)

// FieldError describes a single validation error on an input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem represents an RFC 9457 Problem Details response body.
type Problem struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteJSON encodes v as JSON with the given HTTP status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteProblem writes an RFC 9457 Problem Details response.
func WriteProblem(w http.ResponseWriter, p Problem) {
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	w.Header().Set("Content-Type", ContentTypeProblemJSON)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteError writes a simple RFC 9457 Problem response for a status code and detail message.
func WriteError(w http.ResponseWriter, status int, problemType, title, detail string) {
	WriteProblem(w, Problem{
		Type:   problemType,
		Title:  title,
		Status: status,
		Detail: detail,
	})
}

// WriteValidationError writes a 422 Unprocessable Entity Problem response with field errors.
func WriteValidationError(w http.ResponseWriter, detail string, fieldErrors ...FieldError) {
	WriteProblem(w, Problem{
		Type:   "https://hanayor.com/problems/validation",
		Title:  "Validation failed",
		Status: http.StatusUnprocessableEntity,
		Detail: detail,
		Errors: fieldErrors,
	})
}

// DecodeJSON reads and validates a JSON request body with a 64KB size cap and DisallowUnknownFields.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return fmt.Errorf("request body exceeds %d bytes limit", MaxBodyBytes)
		}
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("request body must not be empty")
		}
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	// Ensure there is no trailing data after the first JSON value.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain only a single JSON object")
	}

	return nil
}

// ErrNotFound indicates the requested domain resource does not exist or is soft-deleted.
var ErrNotFound = errors.New("resource not found")

// ValidationError represents a 422 domain/input validation failure.
type ValidationError struct {
	Detail string
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	return e.Detail
}

// NewValidationError creates a new ValidationError with optional field-level errors.
func NewValidationError(detail string, fields ...FieldError) *ValidationError {
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

// WriteServiceError maps standard domain errors (ValidationError, ConflictError, ErrNotFound)
// to RFC 9457 Problem responses, logging unexpected 500 errors under logEvent.
func WriteServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, logEvent string, err error) {
	var valErr *ValidationError
	if errors.As(err, &valErr) {
		WriteValidationError(w, valErr.Detail, valErr.Fields...)
		return
	}

	var conflictErr *ConflictError
	if errors.As(err, &conflictErr) {
		WriteError(w, http.StatusConflict, "https://hanayor.com/problems/conflict", "Conflict", conflictErr.Detail)
		return
	}

	if errors.Is(err, ErrNotFound) {
		WriteError(w, http.StatusNotFound, "https://hanayor.com/problems/not-found", "Not Found", "The requested resource was not found")
		return
	}

	if logger != nil {
		logger.ErrorContext(r.Context(), logEvent, slog.String("error", err.Error()))
	}
	WriteError(w, http.StatusInternalServerError, "https://hanayor.com/problems/internal-error", "Internal Server Error", "An unexpected error occurred")
}

// ParseYearMonthQuery parses "year" and "month" query parameters.
// When defaultToCurrent is true, missing parameters default to the current year/month in loc.
// When defaultToCurrent is false, missing parameters return a 422 ValidationError.
func ParseYearMonthQuery(r *http.Request, loc *time.Location, defaultToCurrent bool) (int, int, error) {
	rawY := strings.TrimSpace(r.URL.Query().Get("year"))
	rawM := strings.TrimSpace(r.URL.Query().Get("month"))

	if !defaultToCurrent {
		var fields []FieldError
		if rawY == "" {
			fields = append(fields, FieldError{Field: "year", Message: "is required"})
		}
		if rawM == "" {
			fields = append(fields, FieldError{Field: "month", Message: "is required"})
		}
		if len(fields) > 0 {
			return 0, 0, NewValidationError(fields[0].Message, fields...)
		}
	}

	if loc == nil {
		loc = time.FixedZone("JST", 9*3600)
	}
	nowJST := time.Now().In(loc)
	year := nowJST.Year()
	month := int(nowJST.Month())

	if rawY != "" {
		y, err := strconv.Atoi(rawY)
		if err != nil {
			return 0, 0, NewValidationError("year must be an integer", FieldError{Field: "year", Message: "must be an integer"})
		}
		year = y
	}
	if rawM != "" {
		m, err := strconv.Atoi(rawM)
		if err != nil {
			return 0, 0, NewValidationError("month must be an integer", FieldError{Field: "month", Message: "must be an integer"})
		}
		month = m
	}
	return year, month, nil
}

// FormatUUID formats a pgtype.UUID as a canonical 36-char UUID string.
func FormatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ParseUUID parses a canonical UUID string into pgtype.UUID.
func ParseUUID(raw string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(strings.TrimSpace(raw))
	if err != nil || !u.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID %q", raw)
	}
	return u, nil
}

// FormatDate formats a pgtype.Date as YYYY-MM-DD.
func FormatDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

// ParseDate parses a YYYY-MM-DD string into pgtype.Date.
func ParseDate(raw string) (pgtype.Date, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

