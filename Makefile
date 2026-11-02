ifneq (,$(wildcard ./.env))
    include .env
    export
endif

DATABASE_URL ?= postgres://lifeos:lifeos_dev_password@localhost:5432/lifeos_dev?sslmode=disable
TEST_DATABASE_URL ?= postgres://lifeos:lifeos_dev_password@localhost:5432/lifeos_test?sslmode=disable
MIGRATIONS_DIR := backend/db/migrations

.PHONY: help db-up db-down db-init-test migrate-up migrate-down migrate-test sqlc test dev-backend dev-frontend build

help: ## 사용 가능한 Make 명령 출력
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

db-up: ## 로컬 PostgreSQL 18 컨테이너 기동 및 테스트 DB 생성
	docker compose -f compose.dev.yaml up -d --wait
	@docker compose -f compose.dev.yaml exec -T db psql -U $${POSTGRES_USER:-lifeos} -d $${POSTGRES_DB:-lifeos_dev} -tc "SELECT 1 FROM pg_database WHERE datname = 'lifeos_test'" | grep -q 1 || \
		docker compose -f compose.dev.yaml exec -T db psql -U $${POSTGRES_USER:-lifeos} -d $${POSTGRES_DB:-lifeos_dev} -c "CREATE DATABASE lifeos_test;"

db-down: ## 로컬 PostgreSQL 18 컨테이너 중지
	docker compose -f compose.dev.yaml down

migrate-up: ## 개발 DB에 마이그레이션 적용 (up)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down: ## 개발 DB 마이그레이션 1단계 롤백 (down 1)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

migrate-test: ## 테스트 DB에 마이그레이션 적용
	migrate -path $(MIGRATIONS_DIR) -database "$(TEST_DATABASE_URL)" up

sqlc: ## sqlc 코드 생성
	cd backend && sqlc generate

test: ## 백엔드 전체 테스트 실행
	cd backend && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -v -race -count=1 ./...

dev-backend: ## 백엔드 서버 로컬 실행
	cd backend && go run ./cmd/server

dev-frontend: ## 프론트엔드 개발 서버 실행
	cd frontend && pnpm dev

build: ## 백엔드 및 프론트엔드 프로덕션 빌드 확인
	cd backend && go build -o ../bin/server ./cmd/server
	cd frontend && pnpm build

