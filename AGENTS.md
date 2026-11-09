# AGENTS.md — LifeOS Engineering & Design Guidelines

본 문서는 **LifeOS** 저장소에서 작업하는 AI 에이전트와 개발자가 전 Phase에 걸쳐 일관되게 준수해야 하는 아키텍처, 도메인 불변식, UI/UX 디자인 시스템의 핵심 기준이다. 세부 명세는 [`docs/PROJECT_MASTER_PLAN.md`](./docs/PROJECT_MASTER_PLAN.md)와 [`docs/PHASE1_SPECIFICATION.md`](./docs/PHASE1_SPECIFICATION.md)를 참조한다.

---

## 1. 프로젝트 개요 및 엔지니어링 원칙

- **정체성:** 도쿄 거주 1인 사용자를 위한 올인원 라이프 매니지먼트 시스템 (가계부·PayPay 정산·Google Sheets 동기화 → 고정비·예산·공과금·후루사토 납세 → 경제 브리핑·포트폴리오 → 콘서트 추첨 칸반·라이프로그).
- **아키텍처:** **Modular Monolith** (단일 Go 백엔드 + 단일 React 19 PWA + 단일 PostgreSQL 18). 모듈(`ledger`, `living`, `market`, `culture`)은 해당 Phase에 진입할 때만 생성한다(YAGNI).
- **Standard-First & 직접 구현 원칙:**
  - **Backend:** 웹 프레임워크 없이 Go 표준 `net/http` (`http.ServeMux`), `encoding/json`, `log/slog`, `context`, `time`을 사용한다.
  - **Frontend:** 라우팅(History API + `useSyncExternalStore`), 데이터 패칭(`fetch` + `use()` + Suspense + `useOptimistic`), 폼(`useActionState`), 바텀 시트(네이티브 `<dialog>` + Pointer Events), 차트(SVG), 스타일(CSS Custom Properties + CSS Modules)을 외부 라이브러리 없이 직접 구현한다.
- **허용 의존성 기준:** 학습 대상을 가리지 않는 코드 생성기(`sqlc`), 표준 드라이버/인증(`pgx/v5`, `golang.org/x/oauth2`, Google API SDK), 순수 디자인 아이콘(`lucide-react`)만 허용한다. 새 의존성을 추가할 때는 반드시 `docs/PROJECT_MASTER_PLAN.md` §5에 기록한다.
- **정합성:** 구조나 원칙이 사용자에 의해 변경되는 경우 `docs/`하위의 문서들 및 `AGENTS.md`를 최신 상태로 반드시 업데이트한다.

---

## 2. 기술 스택 및 주요 명령

- **툴체인 (`mise.toml`):** Go `1.27.3`, Node.js `24`, pnpm `12.16.1`, `sqlc`, `gomigrate` (`migrate` CLI), PostgreSQL `18` (`postgres:18-alpine`)
- **핵심 Make 타겟:**
  - `make db-up` / `make db-down`: 로컬 PostgreSQL 18(`lifeos_dev`, `lifeos_test`) 기동/중지
  - `make migrate-up` / `make migrate-test`: 개발/테스트 DB 마이그레이션 적용
  - `make sqlc`: `backend/db/queries/*.sql` → `backend/internal/db/` 코드 생성 (**생성된 파일 직접 수정 금지**)
  - `make test`: 백엔드 단위 및 PostgreSQL 통합 테스트 실행 (`-race -count=1`)
  - `make build`: 백엔드 바이너리 및 프론트엔드 프로덕션 번들 빌드 검증

---

## 3. 도메인 및 아키텍처 불변식 (Invariants)

1. **시간대 (Timezone):** 모든 날짜(`tx_date` 기본값, 월 경계, 정기 스케줄, 백업)는 **`Asia/Tokyo` (JST)** 기준이다. DB 타임스탬프는 `TIMESTAMPTZ`로 저장한다.
2. **가계부 회계 모델 (`v_ledger_entries` 단일 기준):**
   - `net_amount = gross_amount - points_used` (`points_used <= gross_amount`)
   - `my_share = net_amount - RECEIVABLE 합계` (`RECEIVABLE 합계 <= net_amount`)
   - 대시보드의 고정비·변동비·비정기 집계는 정산 완료 여부(`settled_at`)와 무관하게 **발생 시점의 `my_share`** 를 기준으로 한다.
   - 동료 대납(`PAYABLE`)은 결제수단 `ADVANCE`(`立替`)로 내 몫만 기록하며, `net_amount`와 동일한 금액의 `PAYABLE` 정산 1건이 반드시 동반되어야 한다(`payment_method == ADVANCE` $\iff$ `PAYABLE` 정산). 정산금 송금·수취 행위 자체는 별도 지출/수입으로 기록하지 않는다.
   - 이미 정산 완료(`settled_at IS NOT NULL`)된 항목이 포함된 트랜잭션의 금액이나 정산 목록 수정 시도는 `409 Conflict`로 거부한다.
3. **다국어/식별자 규칙:**
   - DB(`categories`, `payment_methods` 등)에는 영어 `UPPER_SNAKE_CASE` 식별자(예: `GROCERIES`, `CREDIT_CARD`, `ADVANCE`)만 저장한다.
   - 화면(UI) 및 Google Sheets 동기화 시에만 일본어 표시명(예: `食料品`, `クレジットカード`, `立替`)으로 매핑한다.
4. **API·인증·보안 규약:**
   - 모든 에러 응답은 **RFC 9457 (`application/problem+json`)** 규격을 따르며, 요청 본문은 64KB 제한 및 `DisallowUnknownFields`를 강제한다.
   - 인증은 Google OAuth 2.0(PKCE) + 서버 세션(`sessions` 테이블에 토큰의 SHA-256 해시만 저장, 쿠키 `lifeos_session` 90일 슬라이딩)을 사용한다.
   - 상태 변경 요청(`POST`, `PATCH`, `DELETE`)은 `Origin` 헤더가 `APP_BASE_URL`과 일치하는지 CSRF 검증을 수행한다.
   - `POST /api/v1/transactions`는 `Idempotency-Key`(UUID) 헤더를 통해 모바일 네트워크 재전송 시 중복 생성을 방지한다.
5. **Google Sheets Write-behind (Transactional Outbox):**
   - PostgreSQL이 유일한 원본(Source of Truth)이다. 트랜잭션 생성·수정·삭제 시 동일 DB 트랜잭션 안에서 `sheet_sync_outbox`에 기록하고, 단일 고루틴 워커가 연도별 파일·월별 탭의 `tx_id` 열을 기준으로 멱등 upsert/delete를 수행한다.
6. **보안 및 비밀 관리:**
   - `.env`, `docs/memo.md`, OAuth 시크릿 등은 절대 Git에 커밋하거나 터미널 명령줄 인자·로그에 평문으로 노출하지 않는다.
   - GCP 서비스 계정 키 파일(`.json`)은 발급하지 않으며, VM에서는 ADC(메타데이터 서버), GitHub Actions에서는 Workload Identity Federation만 사용한다.

---

## 4. UI/UX 디자인 시스템 (Frontend Design System)

프론트엔드(`frontend/`) 구현 시 아래 디자인 시스템과 UX 원칙을 엄격히 준수한다.

### 4.1 핵심 디자인 철학 (No "AI-Slop")
- **컨셉:** Monocle, Linear, Stripe 스타일의 절제되고 정교한 모던 에디토리얼 & 핀테크 대시보드.
- **절대 금지 (Anti-AI Rules):** 보라/청록 네온 그라데이션, 과도한 블러/글래스모피즘(`backdrop-blur` 남발), 둥둥 떠 있는 큰 그림자, 무의미한 장식용 배지, 로봇/반짝이 그래픽 및 **이모지(Emoji) 사용 금지**.
- **구분선 중심 레이아웃:** 그림자 대신 `1px solid` 보더(`var(--border-subtle)`)와 여백, 면 분할로 시각적 위계를 잡는다.

### 4.2 컬러 토큰 (`src/styles/tokens.css`)
차가운 순백(`#FFFFFF`)·순흑(`#000000`) 대신 따뜻한 스톤(Stone) 톤 베이스에 브리티시 그린과 골든 오렌지를 조합한다.

| 토큰 역할 | Light Mode | Dark Mode | 용도 |
| :--- | :--- | :--- | :--- |
| `--bg-canvas` | `#FAF9F5` (Warm Alabaster) | `#0B0F0D` (Deep Obsidian Green) | 전체 배경 |
| `--bg-surface` | `#FFFFFF` | `#131A17` | 카드, 바텀 시트 표면 |
| `--text-primary` | `#141A17` | `#ECEBE6` | 본문 및 주요 금액 |
| `--text-muted` | `#5C6460` | `#8A948E` | 보조 설명, 날짜, 라벨 |
| `--border-subtle` | `#E4E2DD` | `#222C27` | `1px solid` 영역/리스트 구분선 |
| `--color-primary` | `#0B3B2D` (Hover `#124E3D`) | `#1E5E4A` | 주요 액션 버튼, 선택 상태 (British Racing Green) |
| `--color-accent` | `#D97706` | `#F59E0B` | 포인트 방어액, 핵심 지표 강조, 미정산/알림 배지 (Golden Orange) |

### 4.3 타이포그래피 & 아이콘
- **숫자 정렬 (필수):** 모든 금액(엔/달러), 퍼센트, 날짜에는 반드시 `font-variant-numeric: tabular-nums`와 모노스페이스 폰트 계열을 적용해 자릿수 흔들림을 방지한다.
- **아이콘:** `lucide-react`만 사용하며, `size={18}`, `strokeWidth={1.75}`로 통일해 날카롭고 세련된 인상을 유지한다.

### 4.4 모바일 UX & 마이크로 인터랙션 (Tactile & Snappy)
- **엄지손가락 동선 (Thumb-zone First):** 하단 네비게이션 및 네이티브 `<dialog>` 기반 지출 입력 바텀 시트(Pointer Events 스와이프 닫기)는 한 손 조작 동선 내에 배치하고, iOS PWA `env(safe-area-inset-*)` 여백을 준수한다.
- **절제된 모션:** 느리고 출렁이는 애니메이션을 금지하고 `150ms cubic-bezier(0.22, 1, 0.36, 1)` 기반의 즉각적인 트랜지션만 적용한다.
- **촉각적 피드백:**
  1. 커스텀 숫자 키패드 및 프리셋 칩 터치 시 즉각적인 스케일 다운(`active` 상태 `transform: scale(0.97)`).
  2. 총액·포인트·더치페이 입력에 따른 실지출(`net_amount`) 및 내 부담액(`my_share`) 실시간 계산 트랜지션.
  3. PayPay 정산 완료 토글 시 골든 오렌지(`--color-accent`) 포인트와 간결한 스트라이크스루(취소선) 애니메이션 적용.

