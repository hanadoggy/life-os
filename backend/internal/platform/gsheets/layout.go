package gsheets

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EnglishMonthNames maps 1-based month numbers to full English month names ("January" .. "December").
var EnglishMonthNames = [13]string{
	"",
	"January",
	"February",
	"March",
	"April",
	"May",
	"June",
	"July",
	"August",
	"September",
	"October",
	"November",
	"December",
}

// FormatMonthTabName resolves a month number (1..12) using the configured tab format.
// Supported placeholders:
//   - "{Month}" or "{month}" -> "January" .. "December"
//   - "{m}"                  -> "1" .. "12"
//   - "{mm}"                 -> "01" .. "12"
func FormatMonthTabName(format string, month int) string {
	if month < 1 || month > 12 {
		return fmt.Sprintf("Month%d", month)
	}
	f := strings.TrimSpace(format)
	if f == "" {
		f = "{Month}"
	}
	f = strings.ReplaceAll(f, "{Month}", EnglishMonthNames[month])
	f = strings.ReplaceAll(f, "{month}", EnglishMonthNames[month])
	f = strings.ReplaceAll(f, "{mm}", fmt.Sprintf("%02d", month))
	f = strings.ReplaceAll(f, "{m}", strconv.Itoa(month))
	return f
}

// ParseMonthFromTabName resolves a 1..12 month number from a tab title ("January", "11月", "Nov", etc.).
func ParseMonthFromTabName(tab string) (int, bool) {
	clean := strings.TrimSpace(tab)
	lower := strings.ToLower(clean)

	for m := 1; m <= 12; m++ {
		engLower := strings.ToLower(EnglishMonthNames[m])
		if lower == engLower || lower == engLower[:3] {
			return m, true
		}
	}

	cleanNum := strings.TrimSuffix(clean, "月")
	cleanNum = strings.TrimSuffix(cleanNum, "월")
	if m, err := strconv.Atoi(strings.TrimSpace(cleanNum)); err == nil && m >= 1 && m <= 12 {
		return m, true
	}
	return 0, false
}

// FormatSheetLocation returns the canonical "<year>/<tab>" location string stored in transactions.sheet_location.
func FormatSheetLocation(year int, tab string) string {
	return fmt.Sprintf("%d/%s", year, strings.TrimSpace(tab))
}

// ParseSheetLocation splits "<year>/<tab>" into (year, tab, ok).
func ParseSheetLocation(loc string) (int, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(loc), "/", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || strings.TrimSpace(parts[1]) == "" {
		return 0, "", false
	}
	return y, strings.TrimSpace(parts[1]), true
}

// CategoryCodeToJapanese maps DB UPPER_SNAKE_CASE category identifiers to Japanese display names for Google Sheets.
var CategoryCodeToJapanese = map[string]string{
	"GROCERIES":             "食料品",
	"SOCIAL":                "交際費",
	"HOUSEHOLD":             "日用品",
	"GAMING":                "ゲーム",
	"CULTURE":               "文化・娯楽",
	"RENT":                  "家賃",
	"UTILITIES":             "光熱費",
	"COMMUNICATION":         "通信費",
	"TRANSPORT":             "交通費",
	"SUBSCRIPTION":          "サブスク",
	"PUBLIC_FEES":           "公共料金・税金",
	"INVESTMENT":            "投資",
	"CLOTHING":              "衣類",
	"FURNITURE_ELECTRONICS": "家具・家電",
	"SELF_DEVELOPMENT":      "自己啓発",
}

// ExpenseTypeCodeToJapanese maps DB expense_type to Japanese display names in Column B.
var ExpenseTypeCodeToJapanese = map[string]string{
	"FIXED":     "固定費",
	"VARIABLE":  "変動費",
	"IRREGULAR": "非定期",
}

// NormalizeExpenseType maps Japanese/Korean/English expense_type labels from Column B to LifeOS codes.
func NormalizeExpenseType(raw string) (kind string, expenseType string) {
	s := strings.TrimSpace(raw)
	upper := strings.ToUpper(s)

	switch {
	case strings.Contains(s, "収入") || strings.Contains(s, "수입") || upper == "INCOME":
		return "INCOME", ""
	case strings.Contains(s, "固定") || strings.Contains(s, "고정") || upper == "FIXED":
		return "EXPENSE", "FIXED"
	case strings.Contains(s, "非定期") || strings.Contains(s, "特別") || strings.Contains(s, "臨時") || strings.Contains(s, "비정기") || upper == "IRREGULAR":
		return "EXPENSE", "IRREGULAR"
	default:
		return "EXPENSE", "VARIABLE"
	}
}

// NormalizeCategoryCode maps varied Japanese/Korean/English category strings from Column C to LifeOS's 15 canonical UPPER_SNAKE_CASE category codes.
// If the input does not match any known alias, it returns ("", false) so the caller can decide how to handle it.
func NormalizeCategoryCode(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "GROCERIES", true
	}
	upper := strings.ToUpper(s)
	if _, ok := CategoryCodeToJapanese[upper]; ok {
		return upper, true
	}

	// Normalize common punctuation/spaces
	compact := strings.NewReplacer(" ", "", "　", "", "・", "", "/", "", "／", "").Replace(s)

	switch {
	// 1. GROCERIES (食料品)
	case strings.Contains(compact, "食料") || strings.Contains(compact, "食品") ||
		strings.Contains(compact, "食費") || strings.Contains(compact, "食用") ||
		strings.Contains(compact, "飲食") || strings.Contains(compact, "外食") ||
		strings.Contains(compact, "スーパー") || strings.Contains(compact, "コンビニ") ||
		strings.Contains(compact, "カフェ") || strings.Contains(compact, "식료") || strings.Contains(compact, "식비") || strings.Contains(compact, "식용"):
		return "GROCERIES", true

	// 2. SOCIAL (交際費)
	case strings.Contains(compact, "交際") || strings.Contains(compact, "飲み会") ||
		strings.Contains(compact, "デート") || strings.Contains(compact, "プレゼント") ||
		strings.Contains(compact, "교제") || strings.Contains(compact, "회식"):
		return "SOCIAL", true

	// 3. HOUSEHOLD (日用品)
	case strings.Contains(compact, "日用") || strings.Contains(compact, "生活用品") ||
		strings.Contains(compact, "雑貨") || strings.Contains(compact, "ドラッグストア") ||
		strings.Contains(compact, "消耗品") || strings.Contains(compact, "생활용품") || strings.Contains(compact, "일용품"):
		return "HOUSEHOLD", true

	// 4. GAMING (ゲーム)
	case strings.Contains(compact, "ゲーム") || strings.Contains(compact, "課金") ||
		strings.Contains(compact, "ガチャ") || strings.Contains(compact, "게임"):
		return "GAMING", true

	// 5. CULTURE (文化・娯楽)
	case strings.Contains(compact, "文化") || strings.Contains(compact, "娯楽") ||
		strings.Contains(compact, "趣味") || strings.Contains(compact, "エンタメ") ||
		strings.Contains(compact, "映画") || strings.Contains(compact, "ライブ") ||
		strings.Contains(compact, "コンサート") || strings.Contains(compact, "推し") ||
		strings.Contains(compact, "イベント") || strings.Contains(compact, "旅行") ||
		strings.Contains(compact, "レジャー") || strings.Contains(compact, "문화") || strings.Contains(compact, "취미"):
		return "CULTURE", true

	// 6. RENT (家賃)
	case strings.Contains(compact, "家賃") || strings.Contains(compact, "住居") ||
		strings.Contains(compact, "住宅") || strings.Contains(compact, "管理費") ||
		strings.Contains(compact, "更新料") || strings.Contains(compact, "월세") || strings.Contains(compact, "주거"):
		return "RENT", true

	// 7. UTILITIES (光熱費)
	case strings.Contains(compact, "光熱") || strings.Contains(compact, "水道") ||
		strings.Contains(compact, "電気") || strings.Contains(compact, "ガス") ||
		strings.Contains(compact, "광열") || strings.Contains(compact, "전기") || strings.Contains(compact, "가스") || strings.Contains(compact, "수도"):
		return "UTILITIES", true

	// 8. COMMUNICATION (通信費)
	case strings.Contains(compact, "通信") || strings.Contains(compact, "スマホ") ||
		strings.Contains(compact, "携帯") || strings.Contains(compact, "ネット") ||
		strings.Contains(compact, "インターネット") || strings.Contains(compact, "回線") ||
		strings.Contains(compact, "Wi-Fi") || strings.Contains(compact, "WiFi") || strings.Contains(compact, "통신"):
		return "COMMUNICATION", true

	// 9. TRANSPORT (交通費)
	case strings.Contains(compact, "交通") || strings.Contains(compact, "電車") ||
		strings.Contains(compact, "定期") || strings.Contains(compact, "Suica") ||
		strings.Contains(compact, "PASMO") || strings.Contains(compact, "タクシー") ||
		strings.Contains(compact, "新幹線") || strings.Contains(compact, "飛行機") ||
		strings.Contains(compact, "バス") || strings.Contains(compact, "교통"):
		return "TRANSPORT", true

	// 10. SUBSCRIPTION (サブスク)
	case strings.Contains(compact, "サブスク") || strings.Contains(compact, "サブスクリプション") ||
		strings.Contains(compact, "月額") || strings.Contains(compact, "구독"):
		return "SUBSCRIPTION", true

	// 11. PUBLIC_FEES (公共料金・税金)
	case strings.Contains(compact, "公共") || strings.Contains(compact, "共通費") ||
		strings.Contains(compact, "共益費") || strings.Contains(compact, "税金") ||
		strings.Contains(compact, "ふるさと") || strings.Contains(compact, "住民税") ||
		strings.Contains(compact, "年金") || strings.Contains(compact, "保険") ||
		strings.Contains(compact, "医療") || strings.Contains(compact, "病院") ||
		strings.Contains(compact, "薬") || strings.Contains(compact, "공공") || strings.Contains(compact, "세금") || strings.Contains(compact, "고향납세") || strings.Contains(compact, "후루사토"):
		return "PUBLIC_FEES", true

	// 12. INVESTMENT (投資)
	case strings.Contains(compact, "投資") || strings.Contains(compact, "NISA") ||
		strings.Contains(compact, "株") || strings.Contains(compact, "積立") ||
		strings.Contains(compact, "証券") || strings.Contains(compact, "貯蓄") || strings.Contains(compact, "투자"):
		return "INVESTMENT", true

	// 13. CLOTHING (衣類)
	case strings.Contains(compact, "衣類") || strings.Contains(compact, "衣服") ||
		strings.Contains(compact, "服") || strings.Contains(compact, "ファッション") ||
		strings.Contains(compact, "アパレル") || strings.Contains(compact, "美容") ||
		strings.Contains(compact, "散髪") || strings.Contains(compact, "美容院") ||
		strings.Contains(compact, "靴") || strings.Contains(compact, "의류") || strings.Contains(compact, "옷"):
		return "CLOTHING", true

	// 14. FURNITURE_ELECTRONICS (家具・家電)
	case strings.Contains(compact, "家具") || strings.Contains(compact, "家電") ||
		strings.Contains(compact, "ガジェット") || strings.Contains(compact, "PC") ||
		strings.Contains(compact, "インテリア") || strings.Contains(compact, "電子機器") ||
		strings.Contains(compact, "가구") || strings.Contains(compact, "가전") || strings.Contains(compact, "전자기기"):
		return "FURNITURE_ELECTRONICS", true

	// 15. SELF_DEVELOPMENT (自己啓発)
	case strings.Contains(compact, "自己啓発") || strings.Contains(compact, "自己開発") ||
		strings.Contains(compact, "自己投資") || strings.Contains(compact, "書籍") ||
		strings.Contains(compact, "本") || strings.Contains(compact, "勉強") ||
		strings.Contains(compact, "教育") || strings.Contains(compact, "資格") ||
		strings.Contains(compact, "学習") || strings.Contains(compact, "セミナー") ||
		strings.Contains(compact, "자기계발") || strings.Contains(compact, "도서"):
		return "SELF_DEVELOPMENT", true
	}

	return "", false
}

// LedgerRowData represents a transaction formatted for writing into a monthly Google Sheet tab.
type LedgerRowData struct {
	TxID            string // UUID string written to TxIDColumn (e.g. Z)
	TxDate          string // Column A: YYYY-MM-DD
	Kind            string // EXPENSE or INCOME
	ExpenseTypeCode string // FIXED, VARIABLE, IRREGULAR
	CategoryCode    string // GROCERIES, COMMUNICATION, etc.
	Title           string // Column D: Store / Item name
	GrossAmount     int32
	PointsUsed      int32
	NetAmount       int32
	ReceivableTotal int32
	MyShare         int32 // Column E: my_share
	Memo            string
}

// FormatRowValues builds the A..F values slice for the monthly sheet tab.
// Column order:
//   - A: Date (YYYY-MM-DD)
//   - B: Expense Type (固定費 / 変動費 / 非定期)
//   - C: Category (食料品, 通信費, etc.)
//   - D: Title / Store (項目)
//   - E: Amount (my_share integer)
//   - F: Memo (with optional accounting breakdown suffix when gross != my_share)
func FormatRowValues(row LedgerRowData) []any {
	colB := "変動費"
	if row.Kind == "INCOME" {
		colB = "収入"
	} else if ja, ok := ExpenseTypeCodeToJapanese[row.ExpenseTypeCode]; ok {
		colB = ja
	}

	colC := row.CategoryCode
	if ja, ok := CategoryCodeToJapanese[row.CategoryCode]; ok {
		colC = ja
	}

	memoText := FormatSheetMemo(row.Memo, row.GrossAmount, row.PointsUsed, row.ReceivableTotal)

	return []any{
		row.TxDate,
		colB,
		colC,
		row.Title,
		row.MyShare,
		memoText,
	}
}

// FormatSheetMemo formats the memo cell, appending "総額 X / P Y / 精算 Z" if points or settlements were used.
func FormatSheetMemo(userMemo string, gross, points, receivable int32) string {
	base := strings.TrimSpace(userMemo)
	if points == 0 && receivable == 0 {
		return base
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("総額 %s", formatCommaInt(gross)))
	if points > 0 {
		parts = append(parts, fmt.Sprintf("P %s", formatCommaInt(points)))
	}
	if receivable > 0 {
		parts = append(parts, fmt.Sprintf("精算 %s", formatCommaInt(receivable)))
	}
	suffix := strings.Join(parts, " / ")
	if base == "" {
		return suffix
	}
	if strings.Contains(base, suffix) {
		return base
	}
	return fmt.Sprintf("%s (%s)", base, suffix)
}

func formatCommaInt(n int32) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.FormatInt(int64(n), 10)
	if len(s) <= 3 {
		return sign + s
	}
	var out []byte
	rem := len(s) % 3
	if rem > 0 {
		out = append(out, s[:rem]...)
		if len(s) > rem {
			out = append(out, ',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		out = append(out, s[i:i+3]...)
		if i+3 < len(s) {
			out = append(out, ',')
		}
	}
	return sign + string(out)
}

// ParsedSheetRow represents a row parsed from an existing monthly sheet tab (A..F).
type ParsedSheetRow struct {
	RowNumber       int // 1-based row number in the sheet
	TxDate          time.Time
	Kind            string
	ExpenseType     string
	CategoryRaw     string
	CategoryCode    string
	CategoryMatched bool
	Title           string
	GrossAmount     int32
	PointsUsed      int32
	Memo            string
	ExistingTxID    string
}

var (
	reNonDigits        = regexp.MustCompile(`[^0-9\-]+`)
	reAllPointsUsed    = regexp.MustCompile(`(?:全て|全部|全額)\s*ポイント(?:使用|利用)?`)
	reNumberBeforePt   = regexp.MustCompile(`([0-9][0-9,]*)\s*(?:円\s*)?ポイント(?:使用|利用)?`)
	reNumberAfterPt    = regexp.MustCompile(`ポイント\s*([0-9][0-9,]*)\s*(?:円)?(?:使用|利用)?`)
)

// ExtractExplicitPointsFromMemo extracts points_used from a memo only when it explicitly states
// either full point usage ("全てポイント使用", "全部ポイント使用", "全額ポイント使用") or a numeric point amount
// with the Japanese word "ポイント" (e.g., "1002ポイント使用", "400円ポイント使用", "ポイント2580円使用").
// It never matches English abbreviations like "S&P500".
func ExtractExplicitPointsFromMemo(memo string, colEAmount int32) int32 {
	s := strings.TrimSpace(memo)
	if s == "" || !strings.Contains(s, "ポイント") || colEAmount <= 0 {
		return 0
	}

	if reAllPointsUsed.MatchString(s) {
		return colEAmount
	}

	if m := reNumberBeforePt.FindStringSubmatch(s); len(m) == 2 {
		if p, err := ParseYenAmount(m[1]); err == nil && p > 0 {
			if p > colEAmount {
				return colEAmount
			}
			return p
		}
	}

	if m := reNumberAfterPt.FindStringSubmatch(s); len(m) == 2 {
		if p, err := ParseYenAmount(m[1]); err == nil && p > 0 {
			if p > colEAmount {
				return colEAmount
			}
			return p
		}
	}

	return 0
}

// ParseSheetRow parses a raw row from a monthly sheet tab (Columns A..F and optional TxID column).
// Returns (row, skip=true, nil) if the row is a header, summary, or blank row.
func ParseSheetRow(rowNumber int, cells []string, txIDColIndex int, defaultYear int, defaultMonth int, parsePoints bool) (ParsedSheetRow, bool, error) {
	getCell := func(idx int) string {
		if idx < 0 || idx >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[idx])
	}

	colA := getCell(0) // Date
	colB := getCell(1) // ExpenseType (固定費/変動費)
	colC := getCell(2) // Category (食料品, 通信費, etc.)
	colD := getCell(3) // Title / Store
	colE := getCell(4) // Amount
	colF := getCell(5) // Memo

	// Skip completely empty rows or header/total rows
	if colA == "" && colD == "" && colE == "" {
		return ParsedSheetRow{}, true, nil
	}
	if isHeaderOrTotalRow(colA, colB, colC, colD, colE) {
		return ParsedSheetRow{}, true, nil
	}

	txDate, err := ParseFlexibleDate(colA, defaultYear, defaultMonth)
	if err != nil {
		return ParsedSheetRow{}, false, fmt.Errorf("row %d: invalid date %q: %w", rowNumber, colA, err)
	}

	amount, err := ParseYenAmount(colE)
	if err != nil {
		return ParsedSheetRow{}, false, fmt.Errorf("row %d: invalid amount %q: %w", rowNumber, colE, err)
	}
	if amount < 0 {
		return ParsedSheetRow{}, false, fmt.Errorf("row %d: negative amount %d is not supported", rowNumber, amount)
	}

	title := colD
	if title == "" {
		if colC != "" {
			title = colC
		} else {
			title = "名称未設定"
		}
	}

	kind, expType := NormalizeExpenseType(colB)
	catCode, matched := NormalizeCategoryCode(colC)

	var points int32
	gross := amount
	if parsePoints && colF != "" && kind == "EXPENSE" {
		points = ExtractExplicitPointsFromMemo(colF, amount)
	}

	existingTxID := ""
	if txIDColIndex >= 0 {
		existingTxID = getCell(txIDColIndex)
	}

	return ParsedSheetRow{
		RowNumber:       rowNumber,
		TxDate:          txDate,
		Kind:            kind,
		ExpenseType:     expType,
		CategoryRaw:     colC,
		CategoryCode:    catCode,
		CategoryMatched: matched,
		Title:           title,
		GrossAmount:     gross,
		PointsUsed:      points,
		Memo:            colF,
		ExistingTxID:    existingTxID,
	}, false, nil
}

func isHeaderOrTotalRow(colA, colB, colC, colD, colE string) bool {
	lowerA := strings.ToLower(colA)
	if lowerA == "date" || strings.Contains(colA, "日付") || strings.Contains(colA, "詳細") ||
		strings.Contains(colA, "날짜") || strings.Contains(colA, "合計") ||
		strings.Contains(colA, "総計") || strings.Contains(colB, "合計") || strings.Contains(colD, "合計") {
		return true
	}
	if colA == "" && (colD == "" || colE == "") {
		return true
	}
	return false
}

// ParseFlexibleDate parses dates like "2025-01-15", "2025/1/15", "1/15", "01-15", "15日", or "15".
func ParseFlexibleDate(raw string, defaultYear int, defaultMonth int) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}

	// Replace Japanese/Korean date markers
	r := strings.NewReplacer("年", "-", "月", "-", "日", "", "년", "-", "월", "-", "일", "", "/", "-", ".", "-")
	norm := strings.Trim(r.Replace(s), "- ")

	// Remove day-of-week suffix like "(月)" or " (Mon)"
	if idx := strings.IndexAny(norm, "(（ "); idx > 0 {
		norm = strings.TrimSpace(norm[:idx])
	}

	parts := strings.Split(norm, "-")
	switch len(parts) {
	case 3:
		y, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		d, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return time.Time{}, fmt.Errorf("cannot parse Y-M-D from %q", raw)
		}
		if y < 100 {
			y += 2000
		}
		return validUTCDate(y, m, d)
	case 2:
		m, err1 := strconv.Atoi(parts[0])
		d, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return time.Time{}, fmt.Errorf("cannot parse M-D from %q", raw)
		}
		return validUTCDate(defaultYear, m, d)
	case 1:
		d, err := strconv.Atoi(parts[0])
		if err != nil {
			return time.Time{}, fmt.Errorf("cannot parse day from %q", raw)
		}
		return validUTCDate(defaultYear, defaultMonth, d)
	default:
		return time.Time{}, fmt.Errorf("unsupported date format %q", raw)
	}
}

func validUTCDate(y, m, d int) (time.Time, error) {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, fmt.Errorf("out of range date %04d-%02d-%02d", y, m, d)
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return time.Time{}, fmt.Errorf("invalid calendar date %04d-%02d-%02d", y, m, d)
	}
	return t, nil
}

// ParseYenAmount parses strings like "¥3,600", "3,600円", "1200" into int32.
func ParseYenAmount(raw string) (int32, error) {
	clean := reNonDigits.ReplaceAllString(strings.TrimSpace(raw), "")
	if clean == "" {
		return 0, fmt.Errorf("empty amount")
	}
	v, err := strconv.ParseInt(clean, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}

// ColumnLetterToIndex converts "A" -> 0, "F" -> 5, "Z" -> 25.
func ColumnLetterToIndex(col string) int {
	col = strings.ToUpper(strings.TrimSpace(col))
	if col == "" {
		return 25
	}
	res := 0
	for i := 0; i < len(col); i++ {
		ch := col[i]
		if ch < 'A' || ch > 'Z' {
			return 25
		}
		res = res*26 + int(ch-'A'+1)
	}
	return res - 1
}
