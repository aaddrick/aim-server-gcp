#!/usr/bin/env bash
# fetch-resend-key.sh <output-path>
#
# Fetches the RESEND_API_KEY secret from Secret Manager using the VM's
# attached service account, via the metadata server — no gcloud CLI needed.
set -euo pipefail

OUT="${1:?usage: fetch-resend-key.sh <output-path>}"
MD="http://metadata.google.internal/computeMetadata/v1"

PROJECT=$(curl -sf -H 'Metadata-Flavor: Google' "$MD/project/project-id")
TOKEN=$(curl -sf -H 'Metadata-Flavor: Google' \
  "$MD/instance/service-accounts/default/token" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

umask 077
curl -sf -H "Authorization: Bearer $TOKEN" \
  "https://secretmanager.googleapis.com/v1/projects/${PROJECT}/secrets/RESEND_API_KEY/versions/latest:access" |
  python3 -c 'import json,sys,base64; sys.stdout.write(base64.b64decode(json.load(sys.stdin)["payload"]["data"]).decode())' \
  > "$OUT"

if [ ! -s "$OUT" ]; then
  echo "error: fetched RESEND_API_KEY is empty" >&2
  exit 1
fi
