package living

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/httpx"
)

// Handler exposes HTTP endpoints for the Living module under /api/v1/living/*.
type Handler struct {
	svc    *Service
	logger *slog.Logger
	loc    *time.Location
}

// NewHandler creates a new Living HTTP handler.
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

// RegisterRoutes mounts all Living endpoints on mux under /api/v1/living.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Monthly Variable Budgets
	mux.HandleFunc("GET /api/v1/living/budgets", h.handleGetBudgetOverview)
	mux.HandleFunc("PUT /api/v1/living/budgets", h.handleUpsertBudget)
	mux.HandleFunc("DELETE /api/v1/living/budgets", h.handleDeleteBudget)

	// Recurring Rules & Subscriptions
	mux.HandleFunc("GET /api/v1/living/recurring-rules", h.handleListRecurringRules)
	mux.HandleFunc("POST /api/v1/living/recurring-rules", h.handleCreateRecurringRule)
	mux.HandleFunc("PATCH /api/v1/living/recurring-rules/{id}", h.handleUpdateRecurringRule)
	mux.HandleFunc("DELETE /api/v1/living/recurring-rules/{id}", h.handleDeleteRecurringRule)
	mux.HandleFunc("POST /api/v1/living/recurring-rules/sync", h.handleSyncRecurringRules)

	// Furusato Nozei (Simplified Estimator & One-stop Tracker)
	mux.HandleFunc("GET /api/v1/living/furusato", h.handleGetFurusatoOverview)
	mux.HandleFunc("PUT /api/v1/living/furusato/profile/{year}", h.handleUpsertFurusatoProfile)
	mux.HandleFunc("POST /api/v1/living/furusato/donations", h.handleCreateFurusatoDonation)
	mux.HandleFunc("PATCH /api/v1/living/furusato/donations/{id}", h.handleUpdateFurusatoDonation)
	mux.HandleFunc("DELETE /api/v1/living/furusato/donations/{id}", h.handleDeleteFurusatoDonation)
}


func (h *Handler) handleGetBudgetOverview(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r, true)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	resp, err := h.svc.GetBudgetOverview(r.Context(), year, month)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleUpsertBudget(w http.ResponseWriter, r *http.Request) {
	var req UpsertBudgetRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.UpsertBudget(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteBudget(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r, false)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	if err := h.svc.DeleteBudget(r.Context(), year, month); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListRecurringRules(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r, true)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	resp, err := h.svc.ListRecurringRules(r.Context(), year, month)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCreateRecurringRule(w http.ResponseWriter, r *http.Request) {
	var req CreateRecurringRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.CreateRecurringRule(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleUpdateRecurringRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid recurring rule id", httpx.FieldError{
			Field:   "id",
			Message: "must be a valid UUID",
		})
		return
	}

	var req UpdateRecurringRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.UpdateRecurringRule(r.Context(), id, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteRecurringRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid recurring rule id", httpx.FieldError{
			Field:   "id",
			Message: "must be a valid UUID",
		})
		return
	}

	if err := h.svc.DeleteRecurringRule(r.Context(), id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSyncRecurringRules(w http.ResponseWriter, r *http.Request) {
	year, month, err := h.parseYearMonthQuery(r, true)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	// Also allow optional JSON body {"year": 2026, "month": 11}
	if r.ContentLength != 0 {
		var req SyncRecurringRulesRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
			httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
			return
		}
		if req.Year != nil {
			year = *req.Year
		}
		if req.Month != nil {
			month = *req.Month
		}
	}

	resp, err := h.svc.SyncRecurringRules(r.Context(), year, month)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleGetFurusatoOverview(w http.ResponseWriter, r *http.Request) {
	year := time.Now().In(h.loc).Year()
	if rawY := strings.TrimSpace(r.URL.Query().Get("year")); rawY != "" {
		parsed, err := strconv.Atoi(rawY)
		if err != nil {
			h.writeServiceError(w, r, newValidationError("year must be an integer", httpx.FieldError{
				Field:   "year",
				Message: "must be an integer",
			}))
			return
		}
		year = parsed
	}

	resp, err := h.svc.GetFurusatoOverview(r.Context(), year)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleUpsertFurusatoProfile(w http.ResponseWriter, r *http.Request) {
	rawYear := strings.TrimSpace(r.PathValue("year"))
	year, err := strconv.Atoi(rawYear)
	if err != nil {
		h.writeServiceError(w, r, newValidationError("year must be an integer", httpx.FieldError{
			Field:   "year",
			Message: "must be an integer",
		}))
		return
	}

	var req UpsertFurusatoProfileRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.UpsertFurusatoProfile(r.Context(), year, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCreateFurusatoDonation(w http.ResponseWriter, r *http.Request) {
	var req CreateFurusatoDonationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.CreateFurusatoDonation(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleUpdateFurusatoDonation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid furusato donation id", httpx.FieldError{
			Field:   "id",
			Message: "must be a valid UUID",
		})
		return
	}

	var req UpdateFurusatoDonationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "https://hanayor.com/problems/bad-request", "Bad Request", err.Error())
		return
	}

	resp, err := h.svc.UpdateFurusatoDonation(r.Context(), id, req)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteFurusatoDonation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"))
	if err != nil {
		httpx.WriteValidationError(w, "invalid furusato donation id", httpx.FieldError{
			Field:   "id",
			Message: "must be a valid UUID",
		})
		return
	}

	if err := h.svc.DeleteFurusatoDonation(r.Context(), id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


func (h *Handler) parseYearMonthQuery(r *http.Request, defaultToCurrent bool) (int, int, error) {
	return httpx.ParseYearMonthQuery(r, h.loc, defaultToCurrent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteServiceError(w, r, h.logger, "living_handler_error", err)
}
