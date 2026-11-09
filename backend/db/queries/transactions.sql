-- name: CreateTransaction :one
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
    metadata,
    source,
    source_ref,
    idempotency_key
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING id;

-- name: GetLedgerEntryByID :one
SELECT
    id,
    tx_date,
    kind,
    expense_type,
    category_id,
    title,
    gross_amount,
    points_used,
    net_amount,
    payment_method_id,
    memo,
    metadata,
    source,
    source_ref,
    idempotency_key,
    sheet_location,
    sheet_synced_at,
    deleted_at,
    created_at,
    updated_at,
    receivable_total,
    my_share
FROM v_ledger_entries
WHERE id = $1;

-- name: GetTransactionSyncMetaByID :one
SELECT
    id,
    tx_date,
    sheet_location,
    deleted_at
FROM transactions
WHERE id = $1;

-- name: GetLedgerEntryByIdempotencyKey :one
SELECT
    id,
    tx_date,
    kind,
    expense_type,
    category_id,
    title,
    gross_amount,
    points_used,
    net_amount,
    payment_method_id,
    memo,
    metadata,
    source,
    source_ref,
    idempotency_key,
    sheet_location,
    sheet_synced_at,
    deleted_at,
    created_at,
    updated_at,
    receivable_total,
    my_share
FROM v_ledger_entries
WHERE idempotency_key = $1;

-- name: ListLedgerEntriesByMonth :many
SELECT
    id,
    tx_date,
    kind,
    expense_type,
    category_id,
    title,
    gross_amount,
    points_used,
    net_amount,
    payment_method_id,
    memo,
    metadata,
    source,
    source_ref,
    idempotency_key,
    sheet_location,
    sheet_synced_at,
    deleted_at,
    created_at,
    updated_at,
    receivable_total,
    my_share
FROM v_ledger_entries
WHERE tx_date >= sqlc.arg(start_date)::date
  AND tx_date < sqlc.arg(end_date)::date
  AND (sqlc.narg(expense_type)::text IS NULL OR expense_type = sqlc.narg(expense_type)::text)
ORDER BY tx_date DESC, created_at DESC, id DESC;

-- name: UpdateTransaction :execrows
UPDATE transactions
SET
    tx_date = $2,
    kind = $3,
    expense_type = $4,
    category_id = $5,
    title = $6,
    gross_amount = $7,
    points_used = $8,
    payment_method_id = $9,
    memo = $10,
    metadata = $11
WHERE id = $1
  AND deleted_at IS NULL;

-- name: SoftDeleteTransaction :execrows
UPDATE transactions
SET deleted_at = now()
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetMonthlyTotals :one
SELECT
    COALESCE(SUM(CASE WHEN kind = 'EXPENSE' AND expense_type = 'FIXED' THEN my_share ELSE 0 END), 0)::INT AS fixed_total,
    COALESCE(SUM(CASE WHEN kind = 'EXPENSE' AND expense_type = 'VARIABLE' THEN my_share ELSE 0 END), 0)::INT AS variable_total,
    COALESCE(SUM(CASE WHEN kind = 'INCOME' THEN my_share ELSE 0 END), 0)::INT AS income_total,
    COALESCE(SUM(CASE WHEN kind = 'EXPENSE' THEN points_used ELSE 0 END), 0)::INT AS points_saved
FROM v_ledger_entries
WHERE tx_date >= sqlc.arg(start_date)::date
  AND tx_date < sqlc.arg(end_date)::date;

-- name: ListDailyVariableCumulative :many
WITH days AS (
    SELECT gs::date AS day
    FROM generate_series(
        sqlc.arg(start_date)::date,
        (sqlc.arg(end_date)::date - INTERVAL '1 day')::date,
        INTERVAL '1 day'
    ) AS gs
),
daily_totals AS (
    SELECT
        tx_date,
        COALESCE(SUM(my_share), 0)::INT AS daily_amount
    FROM v_ledger_entries
    WHERE tx_date >= sqlc.arg(start_date)::date
      AND tx_date < sqlc.arg(end_date)::date
      AND kind = 'EXPENSE'
      AND expense_type = 'VARIABLE'
    GROUP BY tx_date
)
SELECT
    d.day AS date,
    COALESCE(dt.daily_amount, 0)::INT AS amount,
    SUM(COALESCE(dt.daily_amount, 0)) OVER (ORDER BY d.day ASC)::INT AS cumulative
FROM days d
LEFT JOIN daily_totals dt ON dt.tx_date = d.day
ORDER BY d.day ASC;

