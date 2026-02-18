#!/usr/bin/env bash
set -euo pipefail

# Production deploy for License Server on Debian 13 + Caddy.
# Usage example:
#   sudo LICENSE_DOMAIN=license.example.com LICENSE_ADMIN_TOKEN='super-secret' ./deploy/deploy-license-server-debian.sh

APP_NAME="${APP_NAME:-nodax-license-server}"
BIN_NAME="${BIN_NAME:-license-server}"
BIN_SOURCE="${BIN_SOURCE:-./license-server}"
RUN_USER="${RUN_USER:-nodax-license}"
INSTALL_DIR="${INSTALL_DIR:-/opt/nodax-license-server}"
BIN_DIR="$INSTALL_DIR/bin"
BIN_TARGET="$BIN_DIR/$BIN_NAME"
DATA_DIR="${DATA_DIR:-/var/lib/nodax-license-server}"
BACKUP_DIR="$DATA_DIR/backups"
ENV_DIR="${ENV_DIR:-/etc/nodax-license-server}"
ENV_FILE="$ENV_DIR/license-server.env"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
PORT="${PORT:-8091}"
LICENSE_DOMAIN="${LICENSE_DOMAIN:-}"
LICENSE_ADMIN_TOKEN="${LICENSE_ADMIN_TOKEN:-}"
CADDY_FILE="${CADDY_FILE:-/etc/caddy/Caddyfile}"

if [[ "$EUID" -ne 0 ]]; then
  echo "Run as root (sudo)." >&2
  exit 1
fi

if [[ -z "$LICENSE_DOMAIN" ]]; then
  echo "LICENSE_DOMAIN is required (example: license.example.com)." >&2
  exit 1
fi

if [[ ! -f "$BIN_SOURCE" ]]; then
  echo "Binary not found: $BIN_SOURCE" >&2
  echo "Build first: go build -o license-server.exe ./license-server (Windows) or go build -o license-server ./license-server (Linux)." >&2
  exit 1
fi

if [[ -z "$LICENSE_ADMIN_TOKEN" ]]; then
  LICENSE_ADMIN_TOKEN="$(openssl rand -hex 24)"
  echo "[WARN] LICENSE_ADMIN_TOKEN was empty; generated: $LICENSE_ADMIN_TOKEN"
fi

echo "[1/9] Install base packages"
apt-get update -y
apt-get install -y ca-certificates curl

echo "[2/9] Install Caddy (if missing)"
if ! command -v caddy >/dev/null 2>&1; then
  apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -y
  apt-get install -y caddy
fi

echo "[3/9] Prepare user and directories"
id -u "$RUN_USER" >/dev/null 2>&1 || useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$RUN_USER"
mkdir -p "$BIN_DIR" "$DATA_DIR" "$BACKUP_DIR" "$ENV_DIR"

echo "[4/9] Backup old runtime files"
ts="$(date +%Y%m%d-%H%M%S)"
if [[ -f "$BIN_TARGET" ]]; then
  cp "$BIN_TARGET" "$BIN_TARGET.bak-$ts"
fi
if [[ -f "$DATA_DIR/license-server.db" ]]; then
  cp "$DATA_DIR/license-server.db" "$BACKUP_DIR/license-server.db.$ts"
fi
if [[ -f "$DATA_DIR/license-sign.key" ]]; then
  cp "$DATA_DIR/license-sign.key" "$BACKUP_DIR/license-sign.key.$ts"
fi

echo "[5/9] Install binary and env"
install -m 0755 "$BIN_SOURCE" "$BIN_TARGET"
cat > "$ENV_FILE" <<EOF
LICENSE_SERVER_PORT=$PORT
LICENSE_ADMIN_TOKEN=$LICENSE_ADMIN_TOKEN
LICENSE_DATA_DIR=$DATA_DIR
LICENSE_DB_PATH=$DATA_DIR/license-server.db
LICENSE_SIGN_KEY_PATH=$DATA_DIR/license-sign.key
EOF

echo "[6/9] Configure systemd"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=NODAX License Server
After=network.target

[Service]
Type=simple
User=$RUN_USER
WorkingDirectory=$DATA_DIR
EnvironmentFile=$ENV_FILE
ExecStart=$BIN_TARGET
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

chown -R "$RUN_USER":"$RUN_USER" "$DATA_DIR" "$INSTALL_DIR" "$ENV_DIR"
chmod 600 "$ENV_FILE"

echo "[7/9] Configure Caddy reverse proxy"
mkdir -p "$(dirname "$CADDY_FILE")"
if [[ -f "$CADDY_FILE" ]]; then
  cp "$CADDY_FILE" "$CADDY_FILE.bak-$ts"
fi

BLOCK_BEGIN="# BEGIN NODAX-LICENSE-SERVER"
BLOCK_END="# END NODAX-LICENSE-SERVER"
TMP_FILE="$(mktemp)"

if [[ -f "$CADDY_FILE" ]]; then
  awk -v b="$BLOCK_BEGIN" -v e="$BLOCK_END" '
    $0==b {skip=1; next}
    $0==e {skip=0; next}
    skip==0 {print}
  ' "$CADDY_FILE" > "$TMP_FILE"
fi

{
  if [[ -s "$TMP_FILE" ]]; then
    cat "$TMP_FILE"
    echo
  fi
  echo "$BLOCK_BEGIN"
  cat <<EOF
$LICENSE_DOMAIN {
    encode gzip zstd
    reverse_proxy 127.0.0.1:$PORT {
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Host {host}
    }
}
EOF
  echo "$BLOCK_END"
} > "$CADDY_FILE"
rm -f "$TMP_FILE"

caddy validate --config "$CADDY_FILE"

echo "[8/9] Restart services"
systemctl daemon-reload
systemctl enable "$APP_NAME" >/dev/null
systemctl restart "$APP_NAME"
systemctl enable caddy >/dev/null
systemctl restart caddy

echo "[9/9] Health checks"
systemctl is-active --quiet "$APP_NAME"
systemctl is-active --quiet caddy
curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null

echo "[OK] Deploy complete"
echo "Service: $APP_NAME"
echo "Local health: http://127.0.0.1:$PORT/healthz"
echo "Public URL: https://$LICENSE_DOMAIN"
echo "Admin UI: https://$LICENSE_DOMAIN/admin"
echo "Client UI: https://$LICENSE_DOMAIN/client"
echo "Admin token: $LICENSE_ADMIN_TOKEN"
