# LifeOS — Personal Life Management System

도쿄 거주 1인 사용자를 위한 올인원 라이프 매니지먼트 웹 애플리케이션(PWA)입니다.  
현재 **Phase 1 (코어 인프라 & 초고속 가계부)** 및 **Phase 2 (카테고리 분석 & 일본 생활 컨트롤러)** 구현이 완료되었습니다.

---

## 1. 주요 기능

- **초고속 가계부 & PayPay 정산 (`/`, `/settlements`)**
  - 모바일 바텀 시트·커스텀 키패드·프리셋 칩 기반 3초 지출 입력.
  - 영수증 총액(`gross`)·포인트 사용액(`points`)·동료 더치페이(`RECEIVABLE`/`PAYABLE`) 자동 분리 및 **내 부담액(`my_share`)** 기준 집계.
  - 동료별 미정산 잔액 요약 및 원클릭 정산 완료 토글.
- **카테고리 분석 & 예산 컨트롤 (`/` 대시보드)**
  - 고정비(`FIXED`)·변동비(`VARIABLE`) 2분류 체계 및 카테고리별 도넛 차트·요약 리스트.
  - 도넛 차트 ↔ 카테고리 리스트 ↔ 지출 내역 필터 칩 간 양방향 필터링 연동.
  - 월 변동비 예산 설정 및 일별 누적(`Burn-up`) ↔ 예산 잔여(`Burn-down`) 추이 차트.
- **일본 생활 컨트롤러 (`/living`)**
  - **정기 고정비·구독 자동화:** 월간/연간 결제 규칙 스케줄러 자동 기입(서버 재기동 Catch-up 포함) 및 구독 결제일 D-Day 타임라인.
  - **후루사토 납세 트래커:** 총무성 간이 수식 기반 공제 상한액 추정 계산기, 잔여 한도 게이지, 원스톱 특례 신청(온라인 IAM·우편) 완료 관리.
- **Google Sheets 동기화**
  - PostgreSQL을 단일 원본(Source of Truth)으로 유지하며, Transactional Outbox 워커가 연도별 파일·월별 탭에 멱등 반영(Upsert/Delete).

---

## 2. 기술 스택

> **Standard-First:** 웹 프레임워크나 무거운 UI/상태 라이브러리 없이 언어·플랫폼 표준 기능을 우선 사용합니다.

| 영역 | 기술 스택 |
| :--- | :--- |
| **Backend** | Go 1.27 (`net/http`, `log/slog`), `sqlc`, `pgx/v5`, Google OAuth 2.0 (PKCE + 서버 세션) |
| **Database** | PostgreSQL 18 (`uuidv7()`, `v_ledger_entries` 뷰, `golang-migrate`) |
| **Frontend** | React 19, TypeScript, Vite, CSS Modules, 자체 라우터·바텀 시트·SVG 차트, `lucide-react` |
| **Infra** | GCP Compute Engine, Docker Compose, Nginx, GitHub Actions (WIF), GCS 일일 백업 |

---

## 3. 빠른 실행 (`Makefile`)

```bash
make db-up          # 로컬 PostgreSQL 18 (lifeos_dev, lifeos_test) 기동
make migrate-up     # 개발 DB 마이그레이션 적용 (테스트 DB는 make migrate-test)
make sqlc           # SQL 쿼리 -> Go 코드 생성
make test           # 백엔드 단위 및 DB 통합 테스트 실행
make dev-backend    # 백엔드 서버 실행 (:8080)
make dev-frontend   # 프론트엔드 개발 서버 실행 (:5173 -> :8080 프록시)
make build          # 백엔드 바이너리 및 프론트엔드 프로덕션 번들 빌드 검증
```

---

## 4. 문서 및 로드맵

상세 아키텍처, 도메인 불변식, 디자인 시스템은 아래 문서를 참조하세요.

- [`AGENTS.md`](./AGENTS.md) — 엔지니어링 원칙, 회계 불변식, UI/UX 디자인 시스템
- [`docs/PROJECT_MASTER_PLAN.md`](./docs/PROJECT_MASTER_PLAN.md) — 전체 아키텍처 의사결정(ADR) 및 마스터 플랜
- [`docs/PHASE1_SPECIFICATION.md`](./docs/PHASE1_SPECIFICATION.md) — Phase 1 상세 명세 (코어 인프라 & 가계부 MVP)
- [`docs/PHASE2_SPECIFICATION.md`](./docs/PHASE2_SPECIFICATION.md) — Phase 2 상세 명세 (카테고리 분석 & 일본 생활 컨트롤러)

### Roadmap
- [x] **Phase 1: 코어 인프라 구축 & 초고속 가계부 MVP**
- [x] **Phase 2: 가계부 카테고리 분석 고도화 & 일본 생활 컨트롤러** (2분류 개편, 도넛 차트·양방향 필터, 월 예산 Burn-down, 고정비·구독 자동화, 후루사토 납세 트래커)
- [ ] **Phase 3: 투자 터미널** (Gmail 경제 브리핑 수집·아카이브, 관심 종목 52주 레인지 바, NISA/USD 통합 자산 추이)
- [ ] **Phase 4: 컬처 & 라이프로그** (콘서트 티켓 추첨 칸반, 도파민 예산 방어 게이지, 라멘회·러닝 지도 로그북)
