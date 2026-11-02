-- name: ListActiveCategories :many
SELECT id, name, sort_order, is_active
FROM categories
WHERE is_active = TRUE
ORDER BY sort_order ASC, id ASC;

-- name: GetCategoryByID :one
SELECT id, name, sort_order, is_active
FROM categories
WHERE id = $1;

-- name: GetCategoryByName :one
SELECT id, name, sort_order, is_active
FROM categories
WHERE name = $1;

-- name: UpsertCategoryByName :one
INSERT INTO categories (name, sort_order)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, sort_order, is_active;

-- name: ListActivePaymentMethods :many
SELECT id, name, sort_order, is_active
FROM payment_methods
WHERE is_active = TRUE
ORDER BY sort_order ASC, id ASC;

-- name: GetPaymentMethodByID :one
SELECT id, name, sort_order, is_active
FROM payment_methods
WHERE id = $1;

-- name: GetPaymentMethodByName :one
SELECT id, name, sort_order, is_active
FROM payment_methods
WHERE name = $1;

-- name: UpsertPaymentMethodByName :one
INSERT INTO payment_methods (name, sort_order)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, sort_order, is_active;

