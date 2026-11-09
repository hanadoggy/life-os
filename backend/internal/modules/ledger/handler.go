package ledger

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/jackc/pgx/v5/pgtype"
)

// Handler exposes HTTP endpoints for the Ledger module using Go 1.22+ ServeMux patterns.
type Handler struct {
	svc    *Service
	logger *slog.Logger
	loc    *time.Location
}

// NewHandler creates a new Ledger HTTP handler.
func NewHandler(svc *Service, logger *slog.Logger, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.FixedZone("JST", 9*3600)
	}
	return &Handler{
		svc:    svc,
		logger: logger,
		loc:    loc,
	}
}

// RegisterRoutes mounts all Ledger endpoints on mux under /api/v1.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Transactions
	mux.HandleFunc("POST /api/v1/transactions", h.handleCreateTransaction)
	mux.HandleFunc("GET /api/v1/transactions", h.handleListTransactions)
	mux.HandleFunc("GET /api/v1/transactions/{id}", h.handleGetTransaction)
	mux.HandleFunc("PATCH /api/v1/transactions/{id}", h.handleUpdateTransaction)
	mux.HandleFunc("DELETE /api/v1/transactions/{id}", h.handleDeleteTransaction)

	// Monthly Summary
	mux.HandleFunc("GET /api/v1/ledger/summary", h.handleGetMonthlySummary)

	// Reference Data
	mux.HandleFunc("GET /api/v1/categories", h.handleListCategories)
	mux.HandleFunc("GET /api/v1/payment-methods", h.handleListPaymentMethods)

	// Presets
	mux.HandleFunc("GET /api/v1/presets", h.handleListPresets)
	mux.HandleFunc("POST /api/v1/presets", h.handleCreatePreset)
	mux.HandleFunc("PATCH /api/v1/presets/{id}", h.handleUpdatePreset)
	mux.HandleFunc("DELETE /api/v1/presets/{id}", h.handleDeletePreset)

	// Settlements
	mux.HandleFunc("GET /api/v1/settlements", h.handleListSettlements)
	mux.HandleFunc("PATCH /api/v1/settlements/{id}", h.handleUpdateSettlementStatus)
}

func (h *Handler) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	var idemKey *pgtype.UUID
	if rawKey := strings.TrimSpace(r.Header.Get("Idempotency-Key")); rawKey != "" {
		parsedKey, err := parseUUID(rawKey)
		if err != nil {
			httpx.WriteValidationError(w, "Idempotency-Key must be a valid UUID", httpx.FieldError{
				Field:   "Idempotency-Key",
				Message: "must be a valid UUID",
			})
			return
		}
		idemKey = &parsedKey
	}

	var req CreateTransactionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, created, err := h.svc.CreateTransaction(r.Context(), idemKey, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, resp)
}

func (h *Handler) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	var expType *string
	if raw := strings.TrimSpace(r.URL.Query().Get("expense_type")); raw != "" {
		expType = &raw
	}

	var categoryID *int32
	if rawCat := strings.TrimSpace(r.URL.Query().Get("category_id")); rawCat != "" {
		parsedCat, err := strconv.ParseInt(rawCat, 10, 32)
		if err != nil || parsedCat <= 0 {
			httpx.WriteValidationError(w, "category_id must be a positive integer", httpx.FieldError{
				Field:   "category_id",
				Message: "must be a positive integer",
			})
			return
		}
		cid := int32(parsedCat)
		categoryID = &cid
	}

	items, err := h.svc.ListTransactions(r.Context(), year, month, expType, categoryID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (h *Handler) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid transaction id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	resp, err := h.svc.GetTransaction(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleUpdateTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid transaction id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	var req UpdateTransactionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.UpdateTransaction(r.Context(), id, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid transaction id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	if err := h.svc.DeleteTransaction(r.Context(), id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGetMonthlySummary(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	summary, err := h.svc.GetMonthlySummary(r.Context(), year, month)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, summary)
}

func (h *Handler) handleListCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListCategories(r.Context())
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleListPaymentMethods(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPaymentMethods(r.Context())
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleListPresets(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPresets(r.Context())
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleCreatePreset(w http.ResponseWriter, r *http.Request) {
	var req CreatePresetRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	preset, err := h.svc.CreatePreset(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, preset)
}

func (h *Handler) handleUpdatePreset(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid preset id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	var req UpdatePresetRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	preset, err := h.svc.UpdatePreset(r.Context(), id, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preset)
}

func (h *Handler) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid preset id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	if err := h.svc.DeletePreset(r.Context(), id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListSettlements(w http.ResponseWriter, r *http.Request) {
	var settled *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("settled")); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			httpx.WriteValidationError(w, "settled query parameter must be boolean", httpx.FieldError{
				Field:   "settled",
				Message: "must be true or false",
			})
			return
		}
		settled = &b
	}

	resp, err := h.svc.ListSettlements(r.Context(), settled)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleUpdateSettlementStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid settlement id", httpx.FieldError{Field: "id", Message: "must be a valid UUID"})
		return
	}

	var req UpdateSettlementStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}
	if req.Settled == nil {
		httpx.WriteValidationError(w, "settled field is required", httpx.FieldError{
			Field:   "settled",
			Message: "is required",
		})
		return
	}

	resp, err := h.svc.UpdateSettlementStatus(r.Context(), id, *req.Settled)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) parseYearMonthQuery(r *http.Request) (int, int, error) {
	nowJST := time.Now().In(h.loc)
	year := nowJST.Year()
	month := int(nowJST.Month())

	if rawY := strings.TrimSpace(r.URL.Query().Get("year")); rawY != "" {
		y, err := strconv.Atoi(rawY)
		if err != nil {
			return 0, 0, newValidationError("year must be an integer", httpx.FieldError{Field: "year", Message: "must be an integer"})
		}
		year = y
	}
	if rawM := strings.TrimSpace(r.URL.Query().Get("month")); rawM != "" {
		m, err := strconv.Atoi(rawM)
		if err != nil {
			return 0, 0, newValidationError("month must be an integer", httpx.FieldError{Field: "month", Message: "must be an integer"})
		}
		month = m
	}
	return year, month, nil
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var valErr *ValidationError
	if errors.As(err, &valErr) {
		httpx.WriteValidationError(w, valErr.Detail, valErr.Fields...)
		return
	}

	var conflictErr *ConflictError
	if errors.As(err, &conflictErr) {
		httpx.WriteError(w, http.StatusConflict, "https://hanayor.com/problems/conflict", "Conflict", conflictErr.Detail)
		return
	}

	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "https://hanayor.com/problems/not-found", "Not Found", "The requested resource was not found")
		return
	}

	h.logger.ErrorContext(r.Context(), "ledger_handler_error", slog.String("error", err.Error()))
	httpx.WriteError(w, http.StatusInternalServerError, "https://hanayor.com/problems/internal-error", "Internal Server Error", "An unexpected error occurred")
}

