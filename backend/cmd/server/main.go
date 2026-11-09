package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/config"
	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/hanadoggy/life-os/backend/internal/httpx"
	"github.com/hanadoggy/life-os/backend/internal/modules/ledger"
	"github.com/hanadoggy/life-os/backend/internal/modules/living"
	"github.com/hanadoggy/life-os/backend/internal/platform/auth"
	"github.com/hanadoggy/life-os/backend/internal/platform/gsheets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed_to_load_config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		logger.Error("invalid_database_url", slog.String("error", err.Error()))
		os.Exit(1)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET TIME ZONE 'Asia/Tokyo'")
		return err
	}

	dbPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		logger.Error("failed_to_create_db_pool", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	if err := dbPool.Ping(pingCtx); err != nil {
		cancelPing()
		logger.Error("failed_initial_db_ping", slog.String("error", err.Error()))
		os.Exit(1)
	}
	cancelPing()

	queries := db.New(dbPool)
	secureCookie := cfg.IsProduction() || strings.HasPrefix(cfg.AppBaseURL, "https://")
	sessionMgr := auth.NewSessionManager(queries, logger, secureCookie)

	redirectURL := cfg.GoogleOAuthRedirectURL
	if redirectURL == "" {
		redirectURL = cfg.AppBaseURL + "/auth/google/callback"
	}
	authHandler := auth.NewHandler(auth.OAuthConfig{
		ClientID:         cfg.GoogleOAuthClientID,
		ClientSecret:     cfg.GoogleOAuthClientSecret,
		RedirectURL:      redirectURL,
		AllowedUserEmail: cfg.AllowedUserEmail,
		AppBaseURL:       cfg.AppBaseURL,
		SecureCookie:     secureCookie,
	}, sessionMgr, logger)

	sheetWakeCh := make(chan struct{}, 1)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		hCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := dbPool.Ping(hCtx); err != nil {
			logger.ErrorContext(r.Context(), "healthz_db_ping_failed", slog.String("error", err.Error()))
			httpx.WriteError(w,
				http.StatusServiceUnavailable,
				"https://hanayor.com/problems/database-unavailable",
				"Service Unavailable",
				"Database connection check failed",
			)
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
			"time":   time.Now().In(cfg.Location).Format(time.RFC3339),
		})
	})

	// Public OAuth login/callback routes (/auth/google/*)
	authHandler.RegisterPublicRoutes(rootMux)

	// Protected API routes (/api/v1/*) guarded by Session Auth + Origin CSRF
	apiMux := http.NewServeMux()
	authHandler.RegisterProtectedRoutes(apiMux)

	ledgerSvc := ledger.NewService(dbPool, cfg.Location, sheetWakeCh)
	ledgerHandler := ledger.NewHandler(ledgerSvc, logger, cfg.Location)
	ledgerHandler.RegisterRoutes(apiMux)

	livingSvc := living.NewService(dbPool, cfg.Location, sheetWakeCh)
	livingHandler := living.NewHandler(livingSvc, logger, cfg.Location)
	livingHandler.RegisterRoutes(apiMux)

	rootMux.Handle("/api/v1/", httpx.Chain(
		apiMux,
		auth.RequireCSRF(cfg.AppBaseURL),
		auth.RequireAuth(sessionMgr, logger),
	))

	// Background workers lifecycle (Specification §7.3)
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		sessionMgr.RunCleanupWorker(workerCtx)
	}()

	recurringScheduler := living.NewScheduler(livingSvc, logger, cfg.Location)
	wg.Add(1)
	go func() {
		defer wg.Done()
		recurringScheduler.Run(workerCtx)
	}()

	if cfg.SheetsEnabled {
		sheetsClient, err := gsheets.NewADCClient(workerCtx)
		if err != nil {
			logger.Error("failed_to_init_gsheets_client", slog.String("error", err.Error()))
		} else {
			spreadsheetIDs := cfg.SheetsSpreadsheetIDs
			if len(spreadsheetIDs) == 0 {
				discovered, discErr := sheetsClient.DiscoverYearSpreadsheets(workerCtx, "Household account book")
				if discErr != nil {
					logger.Warn("failed_to_autodiscover_spreadsheets", slog.String("error", discErr.Error()))
				} else {
					spreadsheetIDs = discovered
				}
			}
			sheetWorker := gsheets.NewWorker(gsheets.WorkerConfig{
				Enabled:        true,
				SpreadsheetIDs: spreadsheetIDs,
				MonthTabFormat: cfg.SheetsMonthTabFormat,
				TxIDColumn:     cfg.SheetsTxIDColumn,
				AppLogTab:      cfg.SheetsAppLogTab,
				Location:       cfg.Location,
			}, queries, sheetsClient, sheetWakeCh, logger)

			wg.Add(1)
			go func() {
				defer wg.Done()
				sheetWorker.Run(workerCtx)
			}()
		}
	}

	handler := httpx.Chain(
		rootMux,
		httpx.RequestID,
		httpx.Logger(logger),
		httpx.Recover(logger),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server_starting",
			slog.String("addr", srv.Addr),
			slog.String("env", cfg.AppEnv),
			slog.String("tz", cfg.Timezone),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			logger.Error("server_listen_failed", slog.String("error", err.Error()))
			cancelWorkers()
			wg.Wait()
			os.Exit(1)
		}
	case <-ctx.Done():
		stop()
		logger.Info("shutdown_signal_received")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server_graceful_shutdown_failed", slog.String("error", err.Error()))
	}

	cancelWorkers()
	wg.Wait()
	logger.Info("server_stopped_cleanly")
}
