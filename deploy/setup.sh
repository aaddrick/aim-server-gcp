#!/usr/bin/env bash
# setup.sh — provision the AIM server on a fresh Debian VM.
#
# Run as root from a checkout that contains ./deploy and ./signup
# (e.g. after `gcloud compute scp --recurse deploy signup aim-server:~/aim
# --tunnel-through-iap`):
#
#   sudo ./deploy/setup.sh --domain aim.example.com --bucket my-project-aim-backups
#
# Installs and configures:
#   - open-oscar-server (latest GitHub release) as the `openoscar` service
#   - the aim-signup verification service (built from ./signup)
#   - Caddy reverse proxy with automatic HTTPS for the signup site
#   - a daily SQLite backup timer uploading to GCS
#
# Idempotent: safe to re-run for upgrades or config changes.
set -euo pipefail

DOMAIN=""
BUCKET=""
OOS_VERSION=""  # empty = latest release
EMAIL_CAP="90"  # daily verification-email cap; 90 suits Resend's free tier (100/day)

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)    DOMAIN="${2:?}"; shift 2 ;;
    --bucket)    BUCKET="${2:?}"; shift 2 ;;
    --version)   OOS_VERSION="${2:?}"; shift 2 ;;
    --email-cap) EMAIL_CAP="${2:?}"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done
[ -n "$DOMAIN" ] || { echo "usage: setup.sh --domain aim.example.com --bucket BACKUP_BUCKET" >&2; exit 1; }
[ -n "$BUCKET" ] || { echo "usage: setup.sh --domain aim.example.com --bucket BACKUP_BUCKET" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
[ -d "$REPO_DIR/signup" ] || { echo "expected ./signup next to ./deploy" >&2; exit 1; }

echo "==> Installing packages"
apt-get update -qq
apt-get install -y -qq curl sqlite3 golang-go debian-keyring debian-archive-keyring apt-transport-https ca-certificates

if ! command -v caddy >/dev/null; then
  echo "==> Installing Caddy"
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' |
    gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' |
    tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
  apt-get update -qq
  apt-get install -y -qq caddy
fi

echo "==> Creating service users"
id openoscar  >/dev/null 2>&1 || useradd --system --home /var/lib/openoscar  --shell /usr/sbin/nologin openoscar
id aim-signup >/dev/null 2>&1 || useradd --system --home /var/lib/aim-signup --shell /usr/sbin/nologin aim-signup

echo "==> Installing open-oscar-server"
if [ -z "$OOS_VERSION" ]; then
  OOS_VERSION=$(curl -sf https://api.github.com/repos/mk6i/open-oscar-server/releases/latest |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])')
fi
VER_NUM="${OOS_VERSION#v}"
TARBALL="open_oscar_server.${VER_NUM}.linux.x86_64.tar.gz"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT
curl -sfL -o "$TMPDIR/$TARBALL" \
  "https://github.com/mk6i/open-oscar-server/releases/download/${OOS_VERSION}/${TARBALL}"
tar xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"
mkdir -p /opt/openoscar
BIN=$(find "$TMPDIR" -name open_oscar_server -type f | head -n1)
[ -n "$BIN" ] || { echo "binary not found in release tarball" >&2; exit 1; }
install -m 0755 "$BIN" /opt/openoscar/open_oscar_server
echo "    installed ${OOS_VERSION}"

echo "==> Writing open-oscar-server config"
mkdir -p /etc/openoscar
sed "s/@DOMAIN@/${DOMAIN}/g" "$REPO_DIR/deploy/settings.env.template" > /etc/openoscar/settings.env
chmod 0644 /etc/openoscar/settings.env

echo "==> Building aim-signup"
mkdir -p /opt/aim-signup
(cd "$REPO_DIR/signup" && GOCACHE=/tmp/gocache GOFLAGS=-mod=mod go build -o /opt/aim-signup/aim-signup .)
install -m 0755 "$REPO_DIR/deploy/fetch-resend-key.sh" /opt/aim-signup/fetch-resend-key.sh
install -m 0755 "$REPO_DIR/deploy/backup.sh" /opt/aim-signup/backup.sh

echo "==> Writing aim-signup config"
mkdir -p /etc/aim-signup
cat > /etc/aim-signup/config.env <<EOF
SIGNUP_LISTEN=127.0.0.1:8090
BASE_URL=https://${DOMAIN}
MGMT_API_URL=http://127.0.0.1:8080
EMAIL_FROM=AIM Signup <verify@${DOMAIN}>
TOKEN_TTL=24h
DAILY_EMAIL_CAP=${EMAIL_CAP}
EOF
chmod 0644 /etc/aim-signup/config.env

echo "==> Writing backup config"
mkdir -p /etc/aim-backup
cat > /etc/aim-backup/config.env <<EOF
BACKUP_BUCKET=${BUCKET}
DB_PATH=/var/lib/openoscar/oscar.sqlite
LOCAL_DIR=/var/lib/openoscar/backups
EOF
chmod 0644 /etc/aim-backup/config.env

echo "==> Configuring Caddy"
cat > /etc/caddy/Caddyfile <<EOF
${DOMAIN} {
	reverse_proxy 127.0.0.1:8090
}
EOF

echo "==> Installing systemd units"
install -m 0644 "$REPO_DIR/deploy/openoscar.service"  /etc/systemd/system/openoscar.service
install -m 0644 "$REPO_DIR/deploy/aim-signup.service" /etc/systemd/system/aim-signup.service
install -m 0644 "$REPO_DIR/deploy/aim-backup.service" /etc/systemd/system/aim-backup.service
install -m 0644 "$REPO_DIR/deploy/aim-backup.timer"   /etc/systemd/system/aim-backup.timer

systemctl daemon-reload
systemctl enable openoscar aim-signup aim-backup.timer
# restart (not enable --now) so re-runs pick up freshly installed binaries;
# note this drops any active AIM sessions
systemctl restart openoscar aim-signup
systemctl start aim-backup.timer
systemctl reload caddy || systemctl restart caddy

echo
echo "==> Done. Checks:"
echo "    systemctl status openoscar aim-signup"
echo "    curl -s https://${DOMAIN}/healthz"
echo "    AIM clients: host ${DOMAIN}, port 5190"
