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

export TZ="Asia/Tokyo"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
DATESTAMP="$(date +%Y%m%d)"
TMP_DUMP="/tmp/lifeos-${TIMESTAMP}.dump"

trap 'rm -f "${TMP_DUMP}"' EXIT

echo "[backup] Starting PostgreSQL custom-format dump for ${POSTGRES_DB} at ${TIMESTAMP} (JST)..."
docker compose -f compose.prod.yaml exec -T db pg_dump -Fc -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" > "${TMP_DUMP}"

DUMP_SIZE="$(stat -c%s "${TMP_DUMP}" 2>/dev/null || stat -f%z "${TMP_DUMP}")"
if [ "${DUMP_SIZE}" -lt 1024 ]; then
  echo "[backup] ERROR: dump file is suspiciously small (${DUMP_SIZE} bytes)" >&2
  exit 1
fi

DEST_DATED="gs://${BACKUP_BUCKET}/pg/lifeos-${DATESTAMP}.dump"
DEST_LATEST="gs://${BACKUP_BUCKET}/pg/lifeos-latest.dump"

echo "[backup] Uploading (${DUMP_SIZE} bytes) to ${DEST_DATED} and ${DEST_LATEST}..."
gcloud storage cp "${TMP_DUMP}" "${DEST_DATED}" --quiet
gcloud storage cp "${TMP_DUMP}" "${DEST_LATEST}" --quiet

echo "[backup] Completed successfully."

