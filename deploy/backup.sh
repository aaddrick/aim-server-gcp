#!/usr/bin/env bash
# backup.sh — snapshot the open-oscar-server SQLite DB and upload to GCS.
#
# Uses sqlite3's online .backup (safe while the server is running), gzips,
# uploads via the GCS JSON API with the VM service account's token (no
# gcloud CLI required), and keeps the last 7 local copies.
#
# Config via /etc/aim-backup/config.env: BACKUP_BUCKET (required),
# DB_PATH, LOCAL_DIR (optional overrides).
set -euo pipefail

DB_PATH="${DB_PATH:-/var/lib/openoscar/oscar.sqlite}"
LOCAL_DIR="${LOCAL_DIR:-/var/lib/openoscar/backups}"
BACKUP_BUCKET="${BACKUP_BUCKET:?BACKUP_BUCKET is required}"

STAMP=$(date -u +%Y%m%d-%H%M%S)
NAME="oscar-${STAMP}.sqlite.gz"
mkdir -p "$LOCAL_DIR"

TMP=$(mktemp --tmpdir="$LOCAL_DIR" backup-XXXXXX.sqlite)
trap 'rm -f "$TMP"' EXIT

sqlite3 "$DB_PATH" ".backup '$TMP'"
gzip -c "$TMP" > "$LOCAL_DIR/$NAME"

MD="http://metadata.google.internal/computeMetadata/v1"
TOKEN=$(curl -sf -H 'Metadata-Flavor: Google' \
  "$MD/instance/service-accounts/default/token" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

curl -sf -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/gzip" \
  --data-binary @"$LOCAL_DIR/$NAME" \
  "https://storage.googleapis.com/upload/storage/v1/b/${BACKUP_BUCKET}/o?uploadType=media&name=${NAME}" \
  > /dev/null

echo "uploaded gs://${BACKUP_BUCKET}/${NAME}"

# Prune local copies beyond the last 7.
ls -1t "$LOCAL_DIR"/oscar-*.sqlite.gz 2>/dev/null | tail -n +8 | xargs -r rm --
