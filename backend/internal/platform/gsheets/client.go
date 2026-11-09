package gsheets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	sheetsAPIBaseURL = "https://sheets.googleapis.com/v4/spreadsheets"
	driveAPIBaseURL  = "https://www.googleapis.com/drive/v3/files"

	ScopeSpreadsheets = "https://www.googleapis.com/auth/spreadsheets"
	ScopeDriveRead    = "https://www.googleapis.com/auth/drive.readonly"
)

// SheetWriter defines the operations needed by the Outbox Worker and Migration CLI.
// Unit tests replace this with an in-memory FakeSheetWriter.
type SheetWriter interface {
	// ResolveMonthTabName resolves the actual tab name in the spreadsheet for the given month (1..12),
	// matching either the configured format, English month name ("November"), or Japanese month name ("11月").
	ResolveMonthTabName(ctx context.Context, spreadsheetID, tabFormat string, month int) (string, error)

	// UpsertRow finds the row with row.TxID in txIDCol of the given tab; if found, updates it in-place,
	// otherwise appends a new row at the bottom of the monthly table and writes row.TxID to txIDCol.
	UpsertRow(ctx context.Context, spreadsheetID, tabName, txIDCol string, row LedgerRowData) error

	// DeleteRow removes the row matching txID in txIDCol of the given tab via DeleteDimension.
	// If the row is not found, it returns nil (idempotent).
	DeleteRow(ctx context.Context, spreadsheetID, tabName, txIDCol, txID string) error

	// AppendAppLog appends an audit entry to the app_log tab (creating the tab automatically if missing).
	AppendAppLog(ctx context.Context, spreadsheetID, appLogTab, timestamp, op, txID, summary string) error

	// ReadTabRows reads all rows from the given tab (A..txIDCol) as string slices.
	ReadTabRows(ctx context.Context, spreadsheetID, tabName, txIDCol string) ([][]string, error)

	// BatchWriteTxIDs writes tx_id strings into txIDCol for the given 1-based row numbers.
	BatchWriteTxIDs(ctx context.Context, spreadsheetID, tabName, txIDCol string, rowToTxID map[int]string) error
}

// Client implements SheetWriter using the Google Sheets v4 REST API and Application Default Credentials (ADC).
type Client struct {
	httpClient *http.Client
}

// NewADCClient creates a Google Sheets & Drive client authenticated via Application Default Credentials (ADC).
func NewADCClient(ctx context.Context) (*Client, error) {
	httpClient, err := google.DefaultClient(ctx, ScopeSpreadsheets, ScopeDriveRead)
	if err != nil {
		return nil, fmt.Errorf("create google ADC http client: %w", err)
	}
	httpClient.Timeout = 30 * time.Second
	return &Client{httpClient: httpClient}, nil
}

// DiscoverYearSpreadsheets searches Google Drive for the folder named folderName (e.g. "Household account book")
// and returns a map of year (e.g. 2025, 2026) -> spreadsheetId for spreadsheets inside that folder.
func (c *Client) DiscoverYearSpreadsheets(ctx context.Context, folderName string) (map[int]string, error) {
	escapedFolder := strings.ReplaceAll(folderName, "'", "\\'")
	folderQuery := fmt.Sprintf("mimeType = 'application/vnd.google-apps.folder' and name = '%s' and trashed = false", escapedFolder)

	u, _ := url.Parse(driveAPIBaseURL)
	q := u.Query()
	q.Set("q", folderQuery)
	q.Set("fields", "files(id,name)")
	q.Set("pageSize", "10")
	u.RawQuery = q.Encode()

	var folderResp struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u.String(), nil, &folderResp); err != nil {
		return nil, fmt.Errorf("search drive folder %q: %w", folderName, err)
	}
	if len(folderResp.Files) == 0 {
		return nil, fmt.Errorf("drive folder %q not found (make sure it is shared with the service account)", folderName)
	}

	result := make(map[int]string)
	for _, folder := range folderResp.Files {
		fileQuery := fmt.Sprintf("'%s' in parents and mimeType = 'application/vnd.google-apps.spreadsheet' and trashed = false", folder.ID)
		fu, _ := url.Parse(driveAPIBaseURL)
		fq := fu.Query()
		fq.Set("q", fileQuery)
		fq.Set("fields", "files(id,name)")
		fq.Set("pageSize", "50")
		fu.RawQuery = fq.Encode()

		var filesResp struct {
			Files []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"files"`
		}
		if err := c.doJSON(ctx, http.MethodGet, fu.String(), nil, &filesResp); err != nil {
			return nil, fmt.Errorf("list spreadsheets in folder %q: %w", folder.ID, err)
		}
		for _, f := range filesResp.Files {
			if y, err := strconv.Atoi(strings.TrimSpace(f.Name)); err == nil && y >= 2000 && y <= 2100 {
				result[y] = f.ID
			}
		}
	}
	return result, nil
}

// ListSheetTabs returns a map of tab title -> numeric sheetId for the given spreadsheet.
func (c *Client) ListSheetTabs(ctx context.Context, spreadsheetID string) (map[string]int64, error) {
	u := fmt.Sprintf("%s/%s?fields=sheets.properties(sheetId,title)", sheetsAPIBaseURL, url.PathEscape(spreadsheetID))
	var resp struct {
		Sheets []struct {
			Properties struct {
				SheetID int64  `json:"sheetId"`
				Title   string `json:"title"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return nil, err
	}
	tabs := make(map[string]int64, len(resp.Sheets))
	for _, s := range resp.Sheets {
		tabs[s.Properties.Title] = s.Properties.SheetID
	}
	return tabs, nil
}

// ResolveMonthTabName implements SheetWriter.
func (c *Client) ResolveMonthTabName(ctx context.Context, spreadsheetID, tabFormat string, month int) (string, error) {
	tabs, err := c.ListSheetTabs(ctx, spreadsheetID)
	if err != nil {
		return "", err
	}
	candidates := []string{
		FormatMonthTabName(tabFormat, month),
		EnglishMonthNames[month],
		EnglishMonthNames[month][:3],
		fmt.Sprintf("%d月", month),
		fmt.Sprintf("%02d月", month),
		strconv.Itoa(month),
	}
	for _, cand := range candidates {
		for actual := range tabs {
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(cand)) {
				return actual, nil
			}
		}
	}
	return FormatMonthTabName(tabFormat, month), nil
}

// UpsertRow implements SheetWriter.
func (c *Client) UpsertRow(ctx context.Context, spreadsheetID, tabName, txIDCol string, row LedgerRowData) error {
	if err := c.ensureTabExists(ctx, spreadsheetID, tabName); err != nil {
		return err
	}
	txIDCol = normalizeColLetter(txIDCol)
	rowNum, err := c.findRowByTxID(ctx, spreadsheetID, tabName, txIDCol, row.TxID)
	if err != nil {
		return err
	}

	if rowNum == 0 {
		// Find next row after the last non-empty row in A:F
		nextRow, err := c.findNextAvailableRow(ctx, spreadsheetID, tabName)
		if err != nil {
			return err
		}
		rowNum = nextRow
	}

	afRange := fmt.Sprintf("'%s'!A%d:F%d", escapeTabForRange(tabName), rowNum, rowNum)
	txRange := fmt.Sprintf("'%s'!%s%d", escapeTabForRange(tabName), txIDCol, rowNum)

	body := map[string]any{
		"valueInputOption": "USER_ENTERED",
		"data": []map[string]any{
			{
				"range":  afRange,
				"values": [][]any{FormatRowValues(row)},
			},
			{
				"range":  txRange,
				"values": [][]any{{row.TxID}},
			},
		},
	}

	endpoint := fmt.Sprintf("%s/%s/values:batchUpdate", sheetsAPIBaseURL, url.PathEscape(spreadsheetID))
	return c.doJSON(ctx, http.MethodPost, endpoint, body, nil)
}

// DeleteRow implements SheetWriter.
func (c *Client) DeleteRow(ctx context.Context, spreadsheetID, tabName, txIDCol, txID string) error {
	txIDCol = normalizeColLetter(txIDCol)
	rowNum, err := c.findRowByTxID(ctx, spreadsheetID, tabName, txIDCol, txID)
	if err != nil {
		// If the tab itself doesn't exist in this spreadsheet, treat delete as a no-op
		if strings.Contains(err.Error(), "Unable to parse range") {
			return nil
		}
		return err
	}
	if rowNum == 0 {
		return nil // Already absent -> idempotent success
	}

	tabs, err := c.ListSheetTabs(ctx, spreadsheetID)
	if err != nil {
		return err
	}
	sheetID, ok := tabs[tabName]
	if !ok {
		return nil
	}

	body := map[string]any{
		"requests": []map[string]any{
			{
				"deleteDimension": map[string]any{
					"range": map[string]any{
						"sheetId":    sheetID,
						"dimension":  "ROWS",
						"startIndex": rowNum - 1,
						"endIndex":   rowNum,
					},
				},
			},
		},
	}

	endpoint := fmt.Sprintf("%s/%s:batchUpdate", sheetsAPIBaseURL, url.PathEscape(spreadsheetID))
	return c.doJSON(ctx, http.MethodPost, endpoint, body, nil)
}

// AppendAppLog implements SheetWriter.
func (c *Client) AppendAppLog(ctx context.Context, spreadsheetID, appLogTab, timestamp, op, txID, summary string) error {
	if strings.TrimSpace(appLogTab) == "" {
		return nil
	}
	if err := c.ensureTabExists(ctx, spreadsheetID, appLogTab); err != nil {
		return err
	}

	rng := fmt.Sprintf("'%s'!A:D", escapeTabForRange(appLogTab))
	endpoint := fmt.Sprintf(
		"%s/%s/values/%s:append?valueInputOption=USER_ENTERED&insertDataOption=INSERT_ROWS",
		sheetsAPIBaseURL,
		url.PathEscape(spreadsheetID),
		url.PathEscape(rng),
	)
	body := map[string]any{
		"values": [][]any{
			{timestamp, op, txID, summary},
		},
	}
	return c.doJSON(ctx, http.MethodPost, endpoint, body, nil)
}

// ReadTabRows implements SheetWriter.
func (c *Client) ReadTabRows(ctx context.Context, spreadsheetID, tabName, txIDCol string) ([][]string, error) {
	txIDCol = normalizeColLetter(txIDCol)
	rng := fmt.Sprintf("'%s'!A1:%s2000", escapeTabForRange(tabName), txIDCol)
	endpoint := fmt.Sprintf(
		"%s/%s/values/%s?valueRenderOption=FORMATTED_VALUE",
		sheetsAPIBaseURL,
		url.PathEscape(spreadsheetID),
		url.PathEscape(rng),
	)

	var resp struct {
		Values [][]any `json:"values"`
	}
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	rows := make([][]string, len(resp.Values))
	for i, r := range resp.Values {
		strRow := make([]string, len(r))
		for j, cell := range r {
			strRow[j] = fmt.Sprint(cell)
		}
		rows[i] = strRow
	}
	return rows, nil
}

// BatchWriteTxIDs implements SheetWriter.
func (c *Client) BatchWriteTxIDs(ctx context.Context, spreadsheetID, tabName, txIDCol string, rowToTxID map[int]string) error {
	if len(rowToTxID) == 0 {
		return nil
	}
	txIDCol = normalizeColLetter(txIDCol)
	data := make([]map[string]any, 0, len(rowToTxID))
	for rowNum, txID := range rowToTxID {
		rng := fmt.Sprintf("'%s'!%s%d", escapeTabForRange(tabName), txIDCol, rowNum)
		data = append(data, map[string]any{
			"range":  rng,
			"values": [][]any{{txID}},
		})
	}

	body := map[string]any{
		"valueInputOption": "RAW",
		"data":             data,
	}
	endpoint := fmt.Sprintf("%s/%s/values:batchUpdate", sheetsAPIBaseURL, url.PathEscape(spreadsheetID))
	return c.doJSON(ctx, http.MethodPost, endpoint, body, nil)
}

func (c *Client) findRowByTxID(ctx context.Context, spreadsheetID, tabName, txIDCol, txID string) (int, error) {
	rng := fmt.Sprintf("'%s'!%s1:%s2000", escapeTabForRange(tabName), txIDCol, txIDCol)
	endpoint := fmt.Sprintf(
		"%s/%s/values/%s?valueRenderOption=FORMATTED_VALUE",
		sheetsAPIBaseURL,
		url.PathEscape(spreadsheetID),
		url.PathEscape(rng),
	)

	var resp struct {
		Values [][]any `json:"values"`
	}
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return 0, err
	}

	target := strings.TrimSpace(txID)
	for idx, row := range resp.Values {
		if len(row) > 0 && strings.TrimSpace(fmt.Sprint(row[0])) == target {
			return idx + 1, nil
		}
	}
	return 0, nil
}

func (c *Client) findNextAvailableRow(ctx context.Context, spreadsheetID, tabName string) (int, error) {
	rng := fmt.Sprintf("'%s'!A1:F2000", escapeTabForRange(tabName))
	endpoint := fmt.Sprintf(
		"%s/%s/values/%s?valueRenderOption=FORMATTED_VALUE",
		sheetsAPIBaseURL,
		url.PathEscape(spreadsheetID),
		url.PathEscape(rng),
	)

	var resp struct {
		Values [][]any `json:"values"`
	}
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return 0, err
	}

	lastNonEmpty := 0
	for idx, row := range resp.Values {
		for _, cell := range row {
			if strings.TrimSpace(fmt.Sprint(cell)) != "" {
				lastNonEmpty = idx + 1
				break
			}
		}
	}
	if lastNonEmpty == 0 {
		return 2, nil // Assume row 1 is header
	}
	return lastNonEmpty + 1, nil
}

func (c *Client) ensureTabExists(ctx context.Context, spreadsheetID, tabName string) error {
	tabs, err := c.ListSheetTabs(ctx, spreadsheetID)
	if err != nil {
		return err
	}
	if _, exists := tabs[tabName]; exists {
		return nil
	}

	body := map[string]any{
		"requests": []map[string]any{
			{
				"addSheet": map[string]any{
					"properties": map[string]any{
						"title": tabName,
					},
				},
			},
		},
	}
	endpoint := fmt.Sprintf("%s/%s:batchUpdate", sheetsAPIBaseURL, url.PathEscape(spreadsheetID))
	if err := c.doJSON(ctx, http.MethodPost, endpoint, body, nil); err != nil {
		// Ignore if created concurrently
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, reqBody any, respDest any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http %s %s: %w", method, endpoint, err)
	}
	defer res.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("sheets api %s %s returned HTTP %d: %s", method, endpoint, res.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	if respDest != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respDest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func normalizeColLetter(col string) string {
	c := strings.ToUpper(strings.TrimSpace(col))
	if c == "" {
		return "Z"
	}
	return c
}

func escapeTabForRange(tab string) string {
	return strings.ReplaceAll(tab, "'", "''")
}

