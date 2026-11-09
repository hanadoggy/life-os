-- ============================================================================
-- 1. 지출 유형 2원화 (IRREGULAR -> VARIABLE 통합 및 CHECK 제약 갱신)
-- ============================================================================
UPDATE transactions
SET expense_type = 'VARIABLE'
WHERE expense_type = 'IRREGULAR';

UPDATE expense_presets
SET expense_type = 'VARIABLE'
WHERE expense_type = 'IRREGULAR';

ALTER TABLE expense_presets
    DROP CONSTRAINT IF EXISTS expense_presets_expense_type_check,
    ADD CONSTRAINT expense_presets_expense_type_check
        CHECK (expense_type IN ('FIXED', 'VARIABLE'));

ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_expense_type_check,
    ADD CONSTRAINT transactions_expense_type_check
        CHECK (expense_type IN ('FIXED', 'VARIABLE'));

-- ============================================================================
-- 2. 월별 변동비 예산 (monthly_budgets)
--    (year = 0, month = 0) 행은 매월 기본 적용되는 Default 예산으로 사용한다.
-- ============================================================================
CREATE TABLE monthly_budgets (
    year            INT  NOT NULL CHECK (year = 0 OR (year BETWEEN 2020 AND 2100)),
    month           INT  NOT NULL CHECK (month = 0 OR (month BETWEEN 1 AND 12)),
    variable_budget INT  NOT NULL CHECK (variable_budget >= 0),
    memo            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (year, month),
    CONSTRAINT chk_budget_year_month CHECK (
        (year = 0 AND month = 0) OR (year >= 2020 AND month BETWEEN 1 AND 12)
    )
);
CREATE TRIGGER trg_monthly_budgets_updated_at BEFORE UPDATE ON monthly_budgets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================================
-- 3. 정기 고정비 및 구독 규칙 (recurring_rules & recurring_executions)
-- ============================================================================
CREATE TABLE recurring_rules (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    title             TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    expense_type      TEXT NOT NULL DEFAULT 'FIXED' CHECK (expense_type IN ('FIXED', 'VARIABLE')),
    category_id       INT  NOT NULL REFERENCES categories(id),
    amount            INT  NOT NULL CHECK (amount >= 0),
    payment_method_id INT  NOT NULL REFERENCES payment_methods(id),
    frequency         TEXT NOT NULL DEFAULT 'MONTHLY' CHECK (frequency IN ('MONTHLY', 'YEARLY')),
    day_of_month      INT  NOT NULL DEFAULT 1 CHECK (day_of_month BETWEEN 1 AND 31),
    month_of_year     INT  CHECK (month_of_year BETWEEN 1 AND 12),
    auto_create       BOOLEAN NOT NULL DEFAULT TRUE,
    is_subscription   BOOLEAN NOT NULL DEFAULT FALSE,
    memo              TEXT,
    sort_order        INT  NOT NULL DEFAULT 0,
    is_active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_recurring_frequency_month CHECK (
        (frequency = 'MONTHLY' AND month_of_year IS NULL)
        OR (frequency = 'YEARLY' AND month_of_year IS NOT NULL)
    )
);
CREATE TRIGGER trg_recurring_rules_updated_at BEFORE UPDATE ON recurring_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_recurring_rules_active ON recurring_rules (sort_order ASC, created_at ASC)
    WHERE is_active = TRUE;

-- 스케줄러 자동 기입 실행 이력 (rule_id + target_period UNIQUE로 중복 기입 원천 차단)
CREATE TABLE recurring_executions (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    rule_id        UUID NOT NULL REFERENCES recurring_rules(id) ON DELETE CASCADE,
    target_period  TEXT NOT NULL CHECK (target_period ~ '^\d{4}-\d{2}$'), -- 예: "2026-11"
    transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    executed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_id, target_period)
);
CREATE INDEX idx_recurring_executions_period ON recurring_executions (target_period);

-- ============================================================================
-- 4. 후루사토 납세 프로필 및 기부/원스톱 특례 보드 (furusato_profiles & furusato_donations)
-- ============================================================================
CREATE TABLE furusato_profiles (
    tax_year         INT  PRIMARY KEY CHECK (tax_year BETWEEN 2020 AND 2100),
    gross_income     INT  NOT NULL DEFAULT 0 CHECK (gross_income >= 0),     -- 예상 세전 연봉 (엔)
    social_insurance INT  NOT NULL DEFAULT 0 CHECK (social_insurance >= 0), -- 0이면 연봉의 15%로 간이 추정
    ideco_annual     INT  NOT NULL DEFAULT 0 CHECK (ideco_annual >= 0),     -- 연간 iDeCo 납입액 (엔)
    family_status    TEXT NOT NULL DEFAULT 'SINGLE' CHECK (
        family_status IN ('SINGLE', 'MARRIED_NO_DEP', 'MARRIED_DEP_1')
    ),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_furusato_profiles_updated_at BEFORE UPDATE ON furusato_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE furusato_donations (
    id                   UUID PRIMARY KEY DEFAULT uuidv7(),
    tax_year             INT  NOT NULL CHECK (tax_year BETWEEN 2020 AND 2100),
    donated_at           DATE NOT NULL,
    municipality         TEXT NOT NULL CHECK (char_length(municipality) BETWEEN 1 AND 100),
    return_gift          TEXT NOT NULL DEFAULT '' CHECK (char_length(return_gift) <= 200),
    amount               INT  NOT NULL CHECK (amount > 0),
    onestop_method       TEXT NOT NULL DEFAULT 'ONLINE_IAM' CHECK (
        onestop_method IN ('ONLINE_IAM', 'POSTAL', 'TAX_RETURN')
    ),
    onestop_completed    BOOLEAN NOT NULL DEFAULT FALSE,
    completed_at         TIMESTAMPTZ,
    application_deadline DATE NOT NULL, -- 기본값: (tax_year + 1)-01-10
    transaction_id       UUID REFERENCES transactions(id) ON DELETE SET NULL,
    memo                 TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_furusato_donations_updated_at BEFORE UPDATE ON furusato_donations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_furusato_donations_year ON furusato_donations (tax_year, donated_at DESC, id DESC);

