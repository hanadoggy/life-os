package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hanadoggy/life-os/backend/internal/config"
	"github.com/hanadoggy/life-os/backend/internal/db"
	"github.com/hanadoggy/life-os/backend/internal/platform/gsheets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stagedImportRow struct {
	txDate          time.Time
	kind            string
	expenseType     *string
	categoryID      int32
	title           string
	grossAmount     int32
	pointsUsed      int32
	paymentMethodID int32
	memo            *string
	sourceRef       string
	sheetLocation   string
	tabName         string
	rowNumber       int
}

func main() {
	var (
		yearFlag       int
		folderFlag     string
		parsePoints    bool
		dryRun         bool
		writeBackIDs   bool
		discoverOnly   bool
		defaultPayCode string
	)

	flag.IntVar(&yearFlag, "year", 0, "Target year to migrate (e.g. 2025 or 2026). If 0, migrates all configured/discovered years.")
	flag.StringVar(&folderFlag, "folder", "Household account book", "Google Drive folder name containing year spreadsheets when SHEETS_SPREADSHEET_IDS is not set.")
	flag.BoolVar(&parsePoints, "parse-points", false, "Parse point amounts (e.g. P 600) from memo column.")
	flag.BoolVar(&dryRun, "dry-run", false, "Parse and validate rows without writing to PostgreSQL or Google Sheets.")
	flag.BoolVar(&writeBackIDs, "write-back-ids", false, "Write generated tx_id UUIDs back into SHEETS_TXID_COLUMN of each sheet row.")
	flag.BoolVar(&discoverOnly, "discover", false, "Discover and print year=spreadsheetId mappings from Google Drive and exit.")
	flag.StringVar(&defaultPayCode, "default-payment-method", "CREDIT_CARD", "Default payment_methods.name for imported rows.")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sheetsClient, err := gsheets.NewADCClient(ctx)
	if err != nil {
		logger.Error("failed_to_create_google_adc_client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if discoverOnly {
		discovered, err := sheetsClient.DiscoverYearSpreadsheets(ctx, folderFlag)
		if err != nil {
			logger.Error("discover_failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		var years []int
		for y := range discovered {
			years = append(years, y)
		}
		sort.Ints(years)
		var pairs []string
		for _, y := range years {
			pairs = append(pairs, fmt.Sprintf("%d=%s", y, discovered[y]))
			fmt.Printf("Found spreadsheet: %d -> %s\n", y, discovered[y])
		}
		fmt.Printf("\nSHEETS_SPREADSHEET_IDS=%s\n", strings.Join(pairs, ","))
		return
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed_to_load_config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	spreadsheetIDs := cfg.SheetsSpreadsheetIDs
	if len(spreadsheetIDs) == 0 {
		logger.Info("discovering_spreadsheets_from_drive", slog.String("folder", folderFlag))
		discovered, err := sheetsClient.DiscoverYearSpreadsheets(ctx, folderFlag)
		if err != nil {
			logger.Error("failed_to_discover_spreadsheets", slog.String("error", err.Error()))
			os.Exit(1)
		}
		spreadsheetIDs = discovered
	}

	var targetYears []int
	if yearFlag > 0 {
		id, ok := spreadsheetIDs[yearFlag]
		if !ok || id == "" {
			logger.Error("spreadsheet_id_not_found_for_year", slog.Int("year", yearFlag))
			os.Exit(1)
		}
		targetYears = []int{yearFlag}
	} else {
		for y := range spreadsheetIDs {
			targetYears = append(targetYears, y)
		}
		sort.Ints(targetYears)
	}

	if len(targetYears) == 0 {
		logger.Error("no_target_years_to_migrate")
		os.Exit(1)
	}

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed_to_connect_db", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	queries := db.New(dbPool)

	// Load reference maps (categories & payment_methods)
	cats, err := queries.ListActiveCategories(ctx)
	if err != nil {
		logger.Error("failed_to_list_categories", slog.String("error", err.Error()))
		os.Exit(1)
	}
	catIDByName := make(map[string]int32, len(cats))
	for _, c := range cats {
		catIDByName[c.Name] = c.ID
	}

	pms, err := queries.ListActivePaymentMethods(ctx)
	if err != nil {
		logger.Error("failed_to_list_payment_methods", slog.String("error", err.Error()))
		os.Exit(1)
	}
	var defaultPM int32
	for _, pm := range pms {
		if pm.Name == defaultPayCode {
			defaultPM = pm.ID
			break
		}
	}
	if defaultPM == 0 && len(pms) > 0 {
		defaultPM = pms[0].ID
	}

	txIDCol := cfg.SheetsTxIDColumn
	txIDColIdx := gsheets.ColumnLetterToIndex(txIDCol)

	for _, year := range targetYears {
		sheetID := spreadsheetIDs[year]
		if err := migrateYear(
			ctx,
			logger,
			dbPool,
			sheetsClient,
			year,
			sheetID,
			cfg.SheetsMonthTabFormat,
			txIDCol,
			txIDColIdx,
			catIDByName,
			defaultPM,
			parsePoints,
			dryRun,
			writeBackIDs,
		); err != nil {
			logger.Error("migrate_year_failed", slog.Int("year", year), slog.String("error", err.Error()))
			os.Exit(1)
		}
	}
}

func migrateYear(
	ctx context.Context,
	logger *slog.Logger,
	dbPool *pgxpool.Pool,
	sheetsClient *gsheets.Client,
	year int,
	spreadsheetID string,
	tabFormat string,
	txIDCol string,
	txIDColIdx int,
	catIDByName map[string]int32,
	defaultPaymentMethodID int32,
	parsePoints bool,
	dryRun bool,
	writeBackIDs bool,
) error {
	logger.Info("starting_year_migration",
		slog.Int("year", year),
		slog.String("spreadsheet_id", spreadsheetID),
		slog.Bool("dry_run", dryRun),
		slog.Bool("write_back_ids", writeBackIDs),
	)

	existingTabs, err := sheetsClient.ListSheetTabs(ctx, spreadsheetID)
	if err != nil {
		return fmt.Errorf("list sheet tabs: %w", err)
	}

	var staged []stagedImportRow
	unmatchedCategories := make(map[string]int)
	var parseWarnings []string

	for month := 1; month <= 12; month++ {
		tabName := resolveExistingMonthTab(existingTabs, tabFormat, month)
		if tabName == "" {
			continue
		}

		rawRows, err := sheetsClient.ReadTabRows(ctx, spreadsheetID, tabName, txIDCol)
		if err != nil {
			return fmt.Errorf("read tab %s: %w", tabName, err)
		}

		sheetLoc := gsheets.FormatSheetLocation(year, tabName)
		tabCount := 0
		occurrenceCounts := make(map[string]int)

		for idx, cells := range rawRows {
			rowNum := idx + 1
			parsed, skip, pErr := gsheets.ParseSheetRow(rowNum, cells, txIDColIdx, year, month, parsePoints)
			if pErr != nil {
				parseWarnings = append(parseWarnings, fmt.Sprintf("[%s] %v", sheetLoc, pErr))
				continue
			}
			if skip {
				continue
			}
			// Skip rows that already carry a tx_id in SHEETS_TXID_COLUMN (either app-created or previously written back)
			if strings.TrimSpace(parsed.ExistingTxID) != "" {
				continue
			}

			catCode := parsed.CategoryCode
			if !parsed.CategoryMatched || catCode == "" {
				unmatchedCategories[parsed.CategoryRaw]++
				catCode = "GROCERIES"
			}

			catID, ok := catIDByName[catCode]
			if !ok {
				catID = catIDByName["GROCERIES"]
			}

			var expPtr *string
			if parsed.Kind == "EXPENSE" {
				et := parsed.ExpenseType
				if et == "" {
					et = "VARIABLE"
				}
				expPtr = &et
			}

			var memoPtr *string
			if strings.TrimSpace(parsed.Memo) != "" {
				m := strings.TrimSpace(parsed.Memo)
				memoPtr = &m
			}

			sigKey := fmt.Sprintf("%s|%s|%d|%s", parsed.TxDate.Format("2006-01-02"), parsed.Title, parsed.GrossAmount, parsed.Memo)
			occurrenceCounts[sigKey]++
			sourceRef := gsheets.BuildDeterministicSourceRef(
				year,
				tabName,
				parsed.TxDate,
				parsed.Title,
				parsed.GrossAmount,
				parsed.Memo,
				occurrenceCounts[sigKey],
			)

			staged = append(staged, stagedImportRow{
				txDate:          parsed.TxDate,
				kind:            parsed.Kind,
				expenseType:     expPtr,
				categoryID:      catID,
				title:           parsed.Title,
				grossAmount:     parsed.GrossAmount,
				pointsUsed:      parsed.PointsUsed,
				paymentMethodID: defaultPaymentMethodID,
				memo:            memoPtr,
				sourceRef:       sourceRef,
				sheetLocation:   sheetLoc,
				tabName:         tabName,
				rowNumber:       rowNum,
			})
			tabCount++
		}

		logger.Info("parsed_month_tab",
			slog.Int("year", year),
			slog.String("tab", tabName),
			slog.Int("valid_rows", tabCount),
		)
	}

	logger.Info("year_parse_summary",
		slog.Int("year", year),
		slog.Int("total_staged_rows", len(staged)),
		slog.Int("warnings", len(parseWarnings)),
		slog.Int("unmatched_category_variants", len(unmatchedCategories)),
	)

	for _, w := range parseWarnings {
		logger.Warn("row_parse_skipped", slog.String("detail", w))
	}
	for rawCat, cnt := range unmatchedCategories {
		logger.Warn("unmatched_category_fallback_to_groceries",
			slog.String("raw_category", rawCat),
			slog.Int("count", cnt),
		)
	}

	if dryRun || len(staged) == 0 {
		return nil
	}

	// Bulk load via pgx.CopyFrom into a temporary table, then INSERT ... ON CONFLICT (source_ref) DO NOTHING
	tx, err := dbPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin db tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE tmp_sheet_import (
			tx_date           DATE NOT NULL,
			kind              TEXT NOT NULL,
			expense_type      TEXT,
			category_id       INT  NOT NULL,
			title             TEXT NOT NULL,
			gross_amount      INT  NOT NULL,
			points_used       INT  NOT NULL,
			payment_method_id INT  NOT NULL,
			memo              TEXT,
			source_ref        TEXT NOT NULL,
			sheet_location    TEXT NOT NULL
		) ON COMMIT DROP;
	`)
	if err != nil {
		return fmt.Errorf("create temp table: %w", err)
	}

	copyRows := make([][]any, len(staged))
	for i, r := range staged {
		copyRows[i] = []any{
			r.txDate,
			r.kind,
			r.expenseType,
			r.categoryID,
			r.title,
			r.grossAmount,
			r.pointsUsed,
			r.paymentMethodID,
			r.memo,
			r.sourceRef,
			r.sheetLocation,
		}
	}

	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"tmp_sheet_import"},
		[]string{
			"tx_date",
			"kind",
			"expense_type",
			"category_id",
			"title",
			"gross_amount",
			"points_used",
			"payment_method_id",
			"memo",
			"source_ref",
			"sheet_location",
		},
		pgx.CopyFromRows(copyRows),
	)
	if err != nil {
		return fmt.Errorf("copy into temp table: %w", err)
	}

	cmdTag, err := tx.Exec(ctx, `
		INSERT INTO transactions (
			tx_date,
			kind,
			expense_type,
			category_id,
			title,
			gross_amount,
			points_used,
			payment_method_id,
			memo,
			source,
			source_ref,
			sheet_location,
			sheet_synced_at
		)
		SELECT
			tx_date,
			kind,
			expense_type,
			category_id,
			title,
			gross_amount,
			points_used,
			payment_method_id,
			memo,
			'SHEET_IMPORT',
			source_ref,
			sheet_location,
			now()
		FROM tmp_sheet_import
		ON CONFLICT (source_ref) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("insert from temp table: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit import tx: %w", err)
	}

	logger.Info("db_import_completed",
		slog.Int("year", year),
		slog.Int64("inserted_rows", cmdTag.RowsAffected()),
		slog.Int64("skipped_duplicates", int64(len(staged))-cmdTag.RowsAffected()),
	)

	if !writeBackIDs {
		return nil
	}

	// Map each staged source_ref to its current live sheet (tabName, rowNumber) from this run,
	// so we never write UUIDs to stale row numbers if rows shifted earlier.
	sourceRefs := make([]string, len(staged))
	stagedByRef := make(map[string]stagedImportRow, len(staged))
	for i, r := range staged {
		sourceRefs[i] = r.sourceRef
		stagedByRef[r.sourceRef] = r
	}

	rows, err := dbPool.Query(ctx, `
		SELECT id::text, source_ref
		FROM transactions
		WHERE source = 'SHEET_IMPORT'
		  AND source_ref = ANY($1::text[])
		  AND deleted_at IS NULL
	`, sourceRefs)
	if err != nil {
		return fmt.Errorf("query imported ids for write-back: %w", err)
	}
	defer rows.Close()

	byTab := make(map[string]map[int]string)
	for rows.Next() {
		var idStr, srcRef string
		if err := rows.Scan(&idStr, &srcRef); err != nil {
			return err
		}
		stRow, ok := stagedByRef[srcRef]
		if !ok || stRow.rowNumber <= 0 {
			continue
		}
		if byTab[stRow.tabName] == nil {
			byTab[stRow.tabName] = make(map[int]string)
		}
		byTab[stRow.tabName][stRow.rowNumber] = idStr
	}

	for tab, rowMap := range byTab {
		if err := sheetsClient.BatchWriteTxIDs(ctx, spreadsheetID, tab, txIDCol, rowMap); err != nil {
			return fmt.Errorf("write back tx_ids to %d/%s: %w", year, tab, err)
		}
		logger.Info("wrote_back_tx_ids",
			slog.Int("year", year),
			slog.String("tab", tab),
			slog.Int("count", len(rowMap)),
		)
	}

	return nil
}

func resolveExistingMonthTab(existingTabs map[string]int64, tabFormat string, month int) string {
	candidates := []string{
		gsheets.FormatMonthTabName(tabFormat, month),
		gsheets.EnglishMonthNames[month],
		gsheets.EnglishMonthNames[month][:3],
		fmt.Sprintf("%d月", month),
		fmt.Sprintf("%02d月", month),
		strconv.Itoa(month),
	}
	for _, cand := range candidates {
		for actual := range existingTabs {
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(cand)) {
				return actual
			}
		}
	}
	return ""
}

