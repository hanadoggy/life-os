DROP TABLE IF EXISTS furusato_donations;
DROP TABLE IF EXISTS furusato_profiles;
DROP TABLE IF EXISTS recurring_executions;
DROP TABLE IF EXISTS recurring_rules;
DROP TABLE IF EXISTS monthly_budgets;

ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_expense_type_check,
    ADD CONSTRAINT transactions_expense_type_check
        CHECK (expense_type IN ('FIXED', 'VARIABLE', 'IRREGULAR'));

ALTER TABLE expense_presets
    DROP CONSTRAINT IF EXISTS expense_presets_expense_type_check,
    ADD CONSTRAINT expense_presets_expense_type_check
        CHECK (expense_type IN ('FIXED', 'VARIABLE', 'IRREGULAR'));

