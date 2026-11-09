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

