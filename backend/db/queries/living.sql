-- name: GetBudgetByYearMonth :one
SELECT
    year,
    month,
    variable_budget,
    memo,
    created_at,
    updated_at
FROM monthly_budgets
WHERE year = $1 AND month = $2;

-- name: UpsertBudget :one
INSERT INTO monthly_budgets (
    year,
    month,
    variable_budget,
    memo
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (year, month) DO UPDATE SET
    variable_budget = EXCLUDED.variable_budget,
    memo = EXCLUDED.memo
RETURNING
    year,
    month,
    variable_budget,
    memo,
    created_at,
    updated_at;

-- name: DeleteBudget :execrows
DELETE FROM monthly_budgets
WHERE year = $1 AND month = $2;

-- name: ListRecurringRules :many
SELECT
    id,
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active,
    created_at,
    updated_at
FROM recurring_rules
ORDER BY is_active DESC, sort_order ASC, day_of_month ASC, created_at ASC;

-- name: ListActiveAutoRecurringRules :many
SELECT
    id,
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active,
    created_at,
    updated_at
FROM recurring_rules
WHERE is_active = TRUE
  AND auto_create = TRUE
ORDER BY sort_order ASC, day_of_month ASC, created_at ASC;

-- name: GetRecurringRuleByID :one
SELECT
    id,
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active,
    created_at,
    updated_at
FROM recurring_rules
WHERE id = $1;

-- name: CreateRecurringRule :one
INSERT INTO recurring_rules (
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING
    id,
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active,
    created_at,
    updated_at;

-- name: UpdateRecurringRule :one
UPDATE recurring_rules
SET
    title = $2,
    expense_type = $3,
    category_id = $4,
    amount = $5,
    payment_method_id = $6,
    frequency = $7,
    day_of_month = $8,
    month_of_year = $9,
    auto_create = $10,
    is_subscription = $11,
    memo = $12,
    sort_order = $13,
    is_active = $14
WHERE id = $1
RETURNING
    id,
    title,
    expense_type,
    category_id,
    amount,
    payment_method_id,
    frequency,
    day_of_month,
    month_of_year,
    auto_create,
    is_subscription,
    memo,
    sort_order,
    is_active,
    created_at,
    updated_at;

-- name: DeleteRecurringRule :execrows
DELETE FROM recurring_rules
WHERE id = $1;

-- name: ListRecurringExecutionsByPeriod :many
SELECT
    e.id,
    e.rule_id,
    e.target_period,
    e.transaction_id,
    e.executed_at
FROM recurring_executions e
JOIN transactions t ON t.id = e.transaction_id
WHERE e.target_period = $1
  AND t.deleted_at IS NULL;

-- name: GetRecurringExecutionByRuleAndPeriod :one
SELECT
    e.id,
    e.rule_id,
    e.target_period,
    e.transaction_id,
    e.executed_at
FROM recurring_executions e
JOIN transactions t ON t.id = e.transaction_id
WHERE e.rule_id = $1
  AND e.target_period = $2
  AND t.deleted_at IS NULL;

-- name: CreateRecurringExecution :execrows
INSERT INTO recurring_executions (
    rule_id,
    target_period,
    transaction_id
) VALUES (
    $1, $2, $3
)
ON CONFLICT (rule_id, target_period) DO NOTHING;

-- name: GetFurusatoProfileByYear :one
SELECT
    tax_year,
    gross_income,
    social_insurance,
    ideco_annual,
    family_status,
    updated_at
FROM furusato_profiles
WHERE tax_year = $1;

-- name: UpsertFurusatoProfile :one
INSERT INTO furusato_profiles (
    tax_year,
    gross_income,
    social_insurance,
    ideco_annual,
    family_status
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (tax_year) DO UPDATE SET
    gross_income = EXCLUDED.gross_income,
    social_insurance = EXCLUDED.social_insurance,
    ideco_annual = EXCLUDED.ideco_annual,
    family_status = EXCLUDED.family_status
RETURNING
    tax_year,
    gross_income,
    social_insurance,
    ideco_annual,
    family_status,
    updated_at;

-- name: ListFurusatoDonationsByYear :many
SELECT
    d.id,
    d.tax_year,
    d.donated_at,
    d.municipality,
    d.return_gift,
    d.amount,
    d.onestop_method,
    d.onestop_completed,
    d.completed_at,
    d.application_deadline,
    d.transaction_id,
    d.memo,
    d.created_at,
    d.updated_at
FROM furusato_donations d
WHERE d.tax_year = $1
ORDER BY d.donated_at DESC, d.created_at DESC, d.id DESC;

-- name: GetFurusatoDonationByID :one
SELECT
    d.id,
    d.tax_year,
    d.donated_at,
    d.municipality,
    d.return_gift,
    d.amount,
    d.onestop_method,
    d.onestop_completed,
    d.completed_at,
    d.application_deadline,
    d.transaction_id,
    d.memo,
    d.created_at,
    d.updated_at
FROM furusato_donations d
WHERE d.id = $1;

-- name: ClearFurusatoDonationTransactionLink :exec
UPDATE furusato_donations
SET transaction_id = NULL
WHERE transaction_id = $1;

-- name: CreateFurusatoDonation :one
INSERT INTO furusato_donations (
    tax_year,
    donated_at,
    municipality,
    return_gift,
    amount,
    onestop_method,
    onestop_completed,
    completed_at,
    application_deadline,
    transaction_id,
    memo
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING
    id,
    tax_year,
    donated_at,
    municipality,
    return_gift,
    amount,
    onestop_method,
    onestop_completed,
    completed_at,
    application_deadline,
    transaction_id,
    memo,
    created_at,
    updated_at;

-- name: UpdateFurusatoDonation :one
UPDATE furusato_donations
SET
    tax_year = $2,
    donated_at = $3,
    municipality = $4,
    return_gift = $5,
    amount = $6,
    onestop_method = $7,
    onestop_completed = $8,
    completed_at = $9,
    application_deadline = $10,
    memo = $11
WHERE id = $1
RETURNING
    id,
    tax_year,
    donated_at,
    municipality,
    return_gift,
    amount,
    onestop_method,
    onestop_completed,
    completed_at,
    application_deadline,
    transaction_id,
    memo,
    created_at,
    updated_at;

-- name: DeleteFurusatoDonation :execrows
DELETE FROM furusato_donations
WHERE id = $1;



