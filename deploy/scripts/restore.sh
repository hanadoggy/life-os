#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/lifeos}"
cd "${APP_DIR}"

if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

BACKUP_BUCKET="${BACKUP_BUCKET:-lifeos-backups-project-18341a47-a3a8-4e47-a50}"
POSTGRES_USER="${POSTGRES_USER:-lifeos}"
POSTGRES_DB="${POSTGRES_DB:-lifeos}"
DUMP_SOURCE="${1:-gs://${BACKUP_BUCKET}/pg/lifeos-latest.dump}"

TMP_DUMP="/tmp/lifeos-restore-rehearsal.dump"
REHEARSAL_CONTAINER="lifeos-restore-rehearsal-db"

cleanup() {
  rm -f "${TMP_DUMP}"
  docker rm -f "${REHEARSAL_CONTAINER}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[restore-rehearsal] Downloading ${DUMP_SOURCE}..."
gcloud storage cp "${DUMP_SOURCE}" "${TMP_DUMP}" --quiet

echo "[restore-rehearsal] Starting ephemeral PostgreSQL 18 container (${REHEARSAL_CONTAINER})..."
docker rm -f "${REHEARSAL_CONTAINER}" >/dev/null 2>&1 || true
docker run -d --name "${REHEARSAL_CONTAINER}" \
  -e POSTGRES_USER="${POSTGRES_USER}" \
  -e POSTGRES_PASSWORD="rehearsal_password" \
  -e POSTGRES_DB="lifeos_restored" \
  postgres:18-alpine >/dev/null

echo "[restore-rehearsal] Waiting for ephemeral PostgreSQL 18 to become ready..."
for _ in $(seq 1 30); do
  if docker exec "${REHEARSAL_CONTAINER}" pg_isready -U "${POSTGRES_USER}" -d "lifeos_restored" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo "[restore-rehearsal] Running pg_restore into ephemeral container..."
docker exec -i "${REHEARSAL_CONTAINER}" pg_restore -U "${POSTGRES_USER}" -d "lifeos_restored" --no-owner --no-privileges < "${TMP_DUMP}"

LIVE_TX_COUNT="$(docker compose -f compose.prod.yaml exec -T db psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -tAc "SELECT COUNT(*) FROM transactions;")"
RESTORED_TX_COUNT="$(docker exec -i "${REHEARSAL_CONTAINER}" psql -U "${POSTGRES_USER}" -d "lifeos_restored" -tAc "SELECT COUNT(*) FROM transactions;")"

LIVE_CAT_COUNT="$(docker compose -f compose.prod.yaml exec -T db psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -tAc "SELECT COUNT(*) FROM categories;")"
RESTORED_CAT_COUNT="$(docker exec -i "${REHEARSAL_CONTAINER}" psql -U "${POSTGRES_USER}" -d "lifeos_restored" -tAc "SELECT COUNT(*) FROM categories;")"

echo "[restore-rehearsal] Verification Results:"
echo "  - transactions: live=${LIVE_TX_COUNT} | restored=${RESTORED_TX_COUNT}"
echo "  - categories  : live=${LIVE_CAT_COUNT} | restored=${RESTORED_CAT_COUNT}"

if [ "${LIVE_TX_COUNT}" != "${RESTORED_TX_COUNT}" ] || [ "${LIVE_CAT_COUNT}" != "${RESTORED_CAT_COUNT}" ]; then
  echo "[restore-rehearsal] ERROR: Row count mismatch between live DB and restored dump!" >&2
  exit 1
fi

echo "[restore-rehearsal] SUCCESS: Restore rehearsal passed with 100% row count match."

