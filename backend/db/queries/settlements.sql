-- name: CreateSettlement :one
INSERT INTO settlements (
    transaction_id,
    peer_name,
    direction,
    amount,
    method,
    memo
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING
    id,
    transaction_id,
    peer_name,
    direction,
    amount,
    method,
    settled_at,
    memo,
    created_at,
    updated_at;

-- name: ListSettlementsByTransactionID :many
SELECT
    id,
    transaction_id,
    peer_name,
    direction,
    amount,
    method,
    settled_at,
    memo,
    created_at,
    updated_at
FROM settlements
WHERE transaction_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListSettlementsByTransactionIDs :many
SELECT
    id,
    transaction_id,
    peer_name,
    direction,
    amount,
    method,
    settled_at,
    memo,
    created_at,
    updated_at
FROM settlements
WHERE transaction_id = ANY(sqlc.arg(transaction_ids)::uuid[])
ORDER BY created_at ASC, id ASC;

-- name: CountSettledByTransactionID :one
SELECT COUNT(*)::INT
FROM settlements
WHERE transaction_id = $1
  AND settled_at IS NOT NULL;

-- name: DeleteSettlementsByTransactionID :exec
DELETE FROM settlements
WHERE transaction_id = $1;

-- name: ListSettlementsWithTransaction :many
SELECT
    s.id,
    s.transaction_id,
    t.tx_date,
    t.title,
    s.peer_name,
    s.direction,
    s.amount,
    s.method,
    s.settled_at,
    s.memo,
    s.created_at,
    s.updated_at
FROM settlements s
JOIN transactions t ON t.id = s.transaction_id
WHERE t.deleted_at IS NULL
  AND (
      sqlc.narg(settled)::boolean IS NULL
      OR (sqlc.narg(settled)::boolean = TRUE AND s.settled_at IS NOT NULL)
      OR (sqlc.narg(settled)::boolean = FALSE AND s.settled_at IS NULL)
  )
ORDER BY t.tx_date DESC, s.created_at DESC, s.id DESC;

-- name: ListUnsettledBalancesByPeer :many
SELECT
    s.peer_name,
    COALESCE(SUM(CASE WHEN s.direction = 'RECEIVABLE' THEN s.amount ELSE 0 END), 0)::INT AS receivable,
    COALESCE(SUM(CASE WHEN s.direction = 'PAYABLE' THEN s.amount ELSE 0 END), 0)::INT AS payable,
    (
        COALESCE(SUM(CASE WHEN s.direction = 'RECEIVABLE' THEN s.amount ELSE 0 END), 0)
        - COALESCE(SUM(CASE WHEN s.direction = 'PAYABLE' THEN s.amount ELSE 0 END), 0)
    )::INT AS net
FROM settlements s
JOIN transactions t ON t.id = s.transaction_id
WHERE t.deleted_at IS NULL
  AND s.settled_at IS NULL
GROUP BY s.peer_name
ORDER BY s.peer_name ASC;

-- name: GetTotalOutstandingSettlements :one
SELECT
    COALESCE(SUM(CASE WHEN s.direction = 'RECEIVABLE' THEN s.amount ELSE 0 END), 0)::INT AS receivable_outstanding,
    COALESCE(SUM(CASE WHEN s.direction = 'PAYABLE' THEN s.amount ELSE 0 END), 0)::INT AS payable_outstanding
FROM settlements s
JOIN transactions t ON t.id = s.transaction_id
WHERE t.deleted_at IS NULL
  AND s.settled_at IS NULL;

-- name: UpdateSettlementSettledStatus :one
UPDATE settlements s
SET settled_at = CASE WHEN sqlc.arg(settled)::boolean THEN COALESCE(s.settled_at, now()) ELSE NULL END
FROM transactions t
WHERE s.id = sqlc.arg(id)
  AND t.id = s.transaction_id
  AND t.deleted_at IS NULL
RETURNING
    s.id,
    s.transaction_id,
    t.tx_date,
    t.title,
    s.peer_name,
    s.direction,
    s.amount,
    s.method,
    s.settled_at,
    s.memo,
    s.created_at,
    s.updated_at;

