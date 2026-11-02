-- name: ListExpensePresets :many
SELECT
    id,
    title,
    expense_type,
    category_id,
    default_amount,
    default_payment_method_id,
    sort_order,
    created_at,
    updated_at
FROM expense_presets
ORDER BY sort_order ASC, created_at ASC;

-- name: GetExpensePresetByID :one
SELECT
    id,
    title,
    expense_type,
    category_id,
    default_amount,
    default_payment_method_id,
    sort_order,
    created_at,
    updated_at
FROM expense_presets
WHERE id = $1;

-- name: CreateExpensePreset :one
INSERT INTO expense_presets (
    title,
    expense_type,
    category_id,
    default_amount,
    default_payment_method_id,
    sort_order
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING
    id,
    title,
    expense_type,
    category_id,
    default_amount,
    default_payment_method_id,
    sort_order,
    created_at,
    updated_at;

-- name: UpdateExpensePreset :one
UPDATE expense_presets
SET
    title = $2,
    expense_type = $3,
    category_id = $4,
    default_amount = $5,
    default_payment_method_id = $6,
    sort_order = $7
WHERE id = $1
RETURNING
    id,
    title,
    expense_type,
    category_id,
    default_amount,
    default_payment_method_id,
    sort_order,
    created_at,
    updated_at;

-- name: DeleteExpensePreset :execrows
DELETE FROM expense_presets
WHERE id = $1;

