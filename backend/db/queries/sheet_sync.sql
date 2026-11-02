-- name: EnqueueSheetSync :one
INSERT INTO sheet_sync_outbox (
    transaction_id,
    op
) VALUES (
    $1, $2
)
RETURNING id, transaction_id, op, attempts, last_error, next_attempt_at, processed_at, created_at;

-- name: ListPendingSheetSyncOutbox :many
SELECT
    id,
    transaction_id,
    op,
    attempts,
    last_error,
    next_attempt_at,
    processed_at,
    created_at
FROM sheet_sync_outbox
WHERE processed_at IS NULL
  AND next_attempt_at <= now()
ORDER BY next_attempt_at ASC, id ASC
LIMIT $1;

-- name: MarkSheetSyncOutboxProcessed :exec
UPDATE sheet_sync_outbox
SET processed_at = now(),
    last_error = NULL
WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- name: MarkSheetSyncOutboxFailed :exec
UPDATE sheet_sync_outbox
SET attempts = attempts + 1,
    last_error = $2,
    next_attempt_at = $3
WHERE id = $1;

-- name: UpdateTransactionSheetSyncState :exec
UPDATE transactions
SET sheet_location = $2,
    sheet_synced_at = now()
WHERE id = $1;

