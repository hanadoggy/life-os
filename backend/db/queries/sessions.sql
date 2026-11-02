-- name: CreateSession :one
INSERT INTO sessions (
    token_hash,
    email,
    expires_at
) VALUES (
    $1, $2, $3
)
RETURNING token_hash, email, created_at, last_seen_at, expires_at;

-- name: GetActiveSessionByTokenHash :one
SELECT
    token_hash,
    email,
    created_at,
    last_seen_at,
    expires_at
FROM sessions
WHERE token_hash = $1
  AND expires_at > now();

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now(),
    expires_at = $2
WHERE token_hash = $1;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at <= now();

