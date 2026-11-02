package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type samplePayload struct {
		Title  string `json:"title"`
		Amount int    `json:"amount"`
	}

	t.Run("valid payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"Ramen","amount":1200}`))
		rec := httptest.NewRecorder()
		var got samplePayload
		if err := DecodeJSON(rec, req, &got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Title != "Ramen" || got.Amount != 1200 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("rejects unknown fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"Ramen","unknown":true}`))
		rec := httptest.NewRecorder()
		var got samplePayload
		if err := DecodeJSON(rec, req, &got); err == nil {
			t.Fatal("expected error for unknown field, got nil")
		}
	})

	t.Run("rejects oversized body", func(t *testing.T) {
		huge := `{"title":"` + strings.Repeat("a", MaxBodyBytes+100) + `","amount":1}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(huge))
		rec := httptest.NewRecorder()
		var got samplePayload
		if err := DecodeJSON(rec, req, &got); err == nil {
			t.Fatal("expected error for oversized body, got nil")
		}
	})
}

func TestMiddlewareChainAndRecover(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		panic("simulated failure")
	})

	handler := Chain(mux, RequestID, Logger(logger), Recover(logger))

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
	if got := res.Header.Get("X-Request-ID"); got == "" {
		t.Error("expected X-Request-ID header to be set")
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	body, _ := io.ReadAll(res.Body)
	var prob Problem
	if err := json.Unmarshal(body, &prob); err != nil {
		t.Fatalf("failed to decode problem json: %v", err)
	}
	if prob.Status != 500 {
		t.Errorf("problem.Status = %d, want 500", prob.Status)
	}
}

