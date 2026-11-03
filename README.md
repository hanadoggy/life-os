# LifeOS — Personal Life Management System

도쿄 거주 1인 사용자를 위한 올인원 라이프 매니지먼트 시스템입니다.  
현재 **Phase 1 (코어 인프라 구축 & 초고속 가계부 MVP)** 구현이 완료된 상태입니다.

---

## 1. 주요 기능 (Phase 1 MVP)

- **3초 모바일 지출 입력 (PWA):**
  - 네이티브 `<dialog>` 및 Pointer Events 스와이프 닫기 기반 바텀 시트.
  - 커스텀 숫자 키패드 및 자주 쓰는 지출 프리셋 칩 원클릭 적용.
  - 영수증 총액(Gross)과 포인트 사용액(Points) 분리 입력 → 실지출(Net) 실시간 계산.
- **가계부 회계 모델 & 대시보드 (`my_share` 기준):**
  - 동료 더치페이 수취 예정액(`RECEIVABLE`)을 차감한 **내 부담액(`my_share`)** 을 기준으로 대시보드 변동비/고정비/비정기 지출 집계.
  - 동료 대납(`PAYABLE`)은 결제수단 `立替`로 자동 분류.
  - 일별 변동비 누적 추이(Burn-up) 반응형 SVG 차트.
- **PayPay 정산 트래커:**
  - 동료별 미정산 잔액 실시간 요약 및 원클릭 정산 토글 (`useOptimistic` 낙관적 업데이트).
  - 정산 완료 건에 대한 금액 변조 방지 (`409 Conflict`).
- **Google Sheets Write-behind 동기화 (Transactional Outbox):**
  - PostgreSQL을 유일한 원본(Source of Truth)으로 유지.
  - DB 트랜잭션과 함께 Outbox에 기록하고 단일 고루틴 워커가 기존 연도별 파일·월별 탭에 멱등 반영(Upsert/Delete).
  - 과거 시트(`2025`, `2026`) 데이터를 임시 테이블과 `source_ref` 멱등키로 이관하는 마이그레이션 CLI(`cmd/migrate-sheets`).

---

## 2. 기술 스택 & 아키텍처 원칙

> **Standard-First & 불필요한 의존성 최소화 (Pain-driven Adoption)**

| 영역 | 기술 스택 | 비고 |
| :--- | :--- | :--- |
| **Backend** | Go 1.27.3 | 웹 프레임워크 없이 표준 `net/http` (`http.ServeMux`), `log/slog`, `encoding/json` |
| **Database** | PostgreSQL 18 | `uuidv7()` PK, `v_ledger_entries` 뷰, `sqlc`, `pgx/v5` 커넥션 풀 |
| **Frontend** | React 19, TypeScript, Vite | UI/차트/라우팅 라이브러리 없이 `use()`, `useActionState`, `useOptimistic`, SVG 직접 구현 |
| **Styling & Design** | CSS Modules, CSS Tokens | 이모지 배제, British Green & Golden Orange 톤, `tabular-nums` 숫자 정렬 |
| **Security & Auth** | Google OAuth 2.0 (PKCE) | 서버 세션(SHA-256 해시 저장), 90일 슬라이딩 쿠키, Origin 헤더 CSRF 검증, 64KB 본문 제한 |
| **Infra & DevOps** | GCP Compute Engine, Docker Compose, Nginx | WIF(Workload Identity) 무키 GitHub Actions 배포, IAP SSH 터널링, GCS 백업 및 복원 리허설 |

---

## 3. 주요 개발 및 실행 명령 (`Makefile`)

```bash
# 로컬 PostgreSQL 18 (lifeos_dev, lifeos_test) 기동
make db-up

# 개발 DB 마이그레이션 적용
make migrate-up

# sqlc 코드 생성
make sqlc

# 백엔드 단위 및 PostgreSQL 통합 테스트 실행
make test

# 백엔드 개발 서버 실행 (:8080)
make dev-backend

# 프론트엔드 개발 서버 실행 (:5173, Vite Proxy -> :8080)
make dev-frontend

# 백엔드 바이너리 및 프론트엔드 프로덕션 번들 빌드 검증
make build
```

---

## 4. 로드맵 (Roadmap)

- [x] **Phase 1: 코어 인프라 구축 & 초고속 가계부 MVP**
- [ ] **Phase 2: 일본 생활 컨트롤러** (고정비 스케줄러 자동화, 월 예산 Burn-down, 공과금 요금제 비교, 후루사토 납세 트래커)
- [ ] **Phase 3: 투자 터미널** (Gmail 경제 브리핑 수집·아카이브, 관심 종목 52주 레인지 바, NISA/USD 통합 자산 추이)
- [ ] **Phase 4: 컬처 & 라이프로그** (콘서트 티켓 추첨 칸반, 도파민 예산 방어 게이지, 라멘회·러닝 지도 로그북)
