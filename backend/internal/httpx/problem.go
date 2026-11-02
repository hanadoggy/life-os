package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

