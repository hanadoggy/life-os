-- 0. 공통: updated_at 자동 갱신 트리거
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 1. 참조 테이블 (DB에는 영어 UPPER_SNAKE_CASE 식별자 저장, 화면/시트 표시 시 일본어로 매핑)
CREATE TABLE categories (
    id         INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 50),
    sort_order INT  NOT NULL DEFAULT 0,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE
);

-- 기본 카테고리 15종 시드
-- GROCERIES (식료품 / 食料品)
-- SOCIAL (교제비 / 交際費)
-- HOUSEHOLD (생활용품 / 日用品)
-- GAMING (게임 / ゲーム)
-- CULTURE (문화생활 / 文化・娯楽)
-- RENT (월세 / 家賃)
-- UTILITIES (광열비 / 光熱費)
-- COMMUNICATION (통신비 / 通信費)
-- TRANSPORT (교통비 / 交通費)
-- SUBSCRIPTION (구독 / サブスク)
-- PUBLIC_FEES (공공요금·후루사토납세 등 / 公共料金・税金)
-- INVESTMENT (투자 / 投資)
-- CLOTHING (옷 / 衣類)
-- FURNITURE_ELECTRONICS (가구·전자기기 / 家具・家電)
-- SELF_DEVELOPMENT (자기계발 / 自己啓発)
INSERT INTO categories (name, sort_order) VALUES
    ('GROCERIES',             10),
    ('SOCIAL',                20),
    ('HOUSEHOLD',             30),
    ('GAMING',                40),
    ('CULTURE',               50),
    ('RENT',                  60),
    ('UTILITIES',             70),
    ('COMMUNICATION',         80),
    ('TRANSPORT',             90),
    ('SUBSCRIPTION',         100),
    ('PUBLIC_FEES',          110),
    ('INVESTMENT',           120),
    ('CLOTHING',             130),
    ('FURNITURE_ELECTRONICS',140),
    ('SELF_DEVELOPMENT',     150);

CREATE TABLE payment_methods (
    id         INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 50),
    sort_order INT  NOT NULL DEFAULT 0,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE
);

-- 기본 결제수단 4종 + 동료 대납(PAYABLE) 전용 ADVANCE 시드
-- CREDIT_CARD (크레딧카드 / クレジットカード)
-- CASH (현금 / 現金)
-- PAY (페이 / Pay決済)
-- POINT (포인트 / ポイント)
-- ADVANCE (동료 대납 / 立替)
INSERT INTO payment_methods (name, sort_order) VALUES
    ('CREDIT_CARD',  10),
    ('CASH',         20),
    ('PAY',          30),
    ('POINT',        40),
    ('ADVANCE',     999);

-- 2. 자주 쓰는 지출 프리셋
CREATE TABLE expense_presets (
    id                        UUID PRIMARY KEY DEFAULT uuidv7(),
    title                     TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    expense_type              TEXT NOT NULL CHECK (expense_type IN ('FIXED', 'VARIABLE', 'IRREGULAR')),
    category_id               INT  NOT NULL REFERENCES categories(id),
    default_amount            INT  CHECK (default_amount >= 0),
    default_payment_method_id INT  REFERENCES payment_methods(id),
    sort_order                INT  NOT NULL DEFAULT 0,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_presets_updated_at BEFORE UPDATE ON expense_presets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 3. 통합 가계부 트랜잭션
CREATE TABLE transactions (
    id                UUID PRIMARY KEY DEFAULT uuidv7(),
    tx_date           DATE NOT NULL,                               -- JST 기준 날짜
    kind              TEXT NOT NULL DEFAULT 'EXPENSE' CHECK (kind IN ('EXPENSE', 'INCOME')),
    expense_type      TEXT CHECK (expense_type IN ('FIXED', 'VARIABLE', 'IRREGULAR')),  -- INCOME이면 NULL
    category_id       INT  NOT NULL REFERENCES categories(id),
    title             TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),

    -- 금액 로직: 영수증 총액(gross) - 포인트 사용액(points) = 실지출(net)
    -- 내 부담액(my_share) = net - RECEIVABLE 합계 → v_ledger_entries 뷰에서 계산
    gross_amount      INT  NOT NULL CHECK (gross_amount >= 0),
    points_used       INT  NOT NULL DEFAULT 0 CHECK (points_used >= 0),
    net_amount        INT  GENERATED ALWAYS AS (gross_amount - points_used) STORED,

    payment_method_id INT  NOT NULL REFERENCES payment_methods(id),
    memo              TEXT,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- 출처 및 멱등성
    source            TEXT NOT NULL DEFAULT 'APP' CHECK (source IN ('APP', 'SHEET_IMPORT')),
    source_ref        TEXT UNIQUE,                                  -- 예: "2025/11月/B12" (SHEET_IMPORT 전용)
    idempotency_key   UUID UNIQUE,                                  -- POST 재전송 방지 (APP 전용)

    -- Google Sheets 반영 상태
    sheet_location    TEXT,                                         -- 예: "2026/11月"
    sheet_synced_at   TIMESTAMPTZ,

    deleted_at        TIMESTAMPTZ,                                  -- 소프트 삭제
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_points_le_gross CHECK (points_used <= gross_amount),
    CONSTRAINT chk_kind_expense_type CHECK (
        (kind = 'EXPENSE' AND expense_type IS NOT NULL)
        OR (kind = 'INCOME' AND expense_type IS NULL AND points_used = 0)
    ),
    CONSTRAINT chk_source_ref CHECK ((source = 'SHEET_IMPORT') = (source_ref IS NOT NULL))
);
CREATE TRIGGER trg_transactions_updated_at BEFORE UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_transactions_date ON transactions (tx_date DESC, id DESC) WHERE deleted_at IS NULL;

-- 4. 동료 간 더치페이 / PayPay 정산
CREATE TABLE settlements (
    id             UUID PRIMARY KEY DEFAULT uuidv7(),
    transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    peer_name      TEXT NOT NULL CHECK (char_length(peer_name) BETWEEN 1 AND 50),
    direction      TEXT NOT NULL CHECK (direction IN ('RECEIVABLE', 'PAYABLE')),
    amount         INT  NOT NULL CHECK (amount > 0),
    method         TEXT NOT NULL DEFAULT 'PayPay',
    settled_at     TIMESTAMPTZ,                                    -- NULL = 미정산
    memo           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_settlements_updated_at BEFORE UPDATE ON settlements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_settlements_transaction ON settlements (transaction_id);
CREATE INDEX idx_settlements_unsettled   ON settlements (created_at DESC) WHERE settled_at IS NULL;

-- 5. 회계 뷰: 내 부담액(my_share) 계산 (정산 반영 집계의 단일 기준)
CREATE VIEW v_ledger_entries AS
SELECT
    t.*,
    COALESCE(r.receivable_total, 0)                AS receivable_total,
    t.net_amount - COALESCE(r.receivable_total, 0) AS my_share
FROM transactions t
LEFT JOIN (
    SELECT transaction_id, SUM(amount)::INT AS receivable_total
    FROM settlements
    WHERE direction = 'RECEIVABLE'
    GROUP BY transaction_id
) r ON r.transaction_id = t.id
WHERE t.deleted_at IS NULL;

-- 6. 세션 (서버 측 세션 저장소)
CREATE TABLE sessions (
    token_hash   BYTEA PRIMARY KEY,                  -- SHA-256(쿠키 토큰)
    email        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

-- 7. Google Sheets 동기화 Outbox (Transactional Outbox 패턴)
CREATE TABLE sheet_sync_outbox (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id  UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    op              TEXT NOT NULL CHECK (op IN ('UPSERT', 'DELETE')),
    attempts        INT  NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_pending ON sheet_sync_outbox (next_attempt_at, id) WHERE processed_at IS NULL;

