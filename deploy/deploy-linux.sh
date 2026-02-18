#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="${APP_NAME:-nodax-central}"
INSTALL_DIR="${INSTALL_DIR:-/opt/nodax-central}"
BIN_DIR="$INSTALL_DIR/bin"
BIN_TARGET="$BIN_DIR/$APP_NAME"
DATA_DIR="${DATA_DIR:-/var/lib/nodax-central}"
BACKUP_DIR="$DATA_DIR/backups"
ENV_DIR="${ENV_DIR:-/etc/nodax-central}"
ENV_FILE="$ENV_DIR/nodax-central.env"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
PORT="${PORT:-8080}"
BIN_SOURCE="${BIN_SOURCE:-./$APP_NAME}"
RUN_USER="${RUN_USER:-nodax}"
SETUP_CADDY="${SETUP_CADDY:-0}"
CADDY_DOMAIN="${CADDY_DOMAIN:-}"
CADDY_CONFIG_DIR="${CADDY_CONFIG_DIR:-/etc/caddy}"
CADDYFILE_PATH="$CADDY_CONFIG_DIR/Caddyfile"
CADDY_TEMPLATE="${CADDY_TEMPLATE:-$SCRIPT_DIR/Caddyfile.linux}"

if [[ "$EUID" -ne 0 ]]; then
  echo "Запустите скрипт от root (sudo)." >&2
  exit 1
fi

if [[ ! -f "$BIN_SOURCE" ]]; then
  echo "Бинарник не найден: $BIN_SOURCE" >&2
  exit 1
fi

if [[ "$SETUP_CADDY" == "1" ]] && [[ -z "$CADDY_DOMAIN" ]]; then
  echo "Для автонастройки Caddy укажите CADDY_DOMAIN (например, central.example.com)." >&2
  exit 1
fi

echo "[1/10] Подготовка пользователя и директорий"
id -u "$RUN_USER" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
mkdir -p "$BIN_DIR" "$DATA_DIR" "$BACKUP_DIR" "$ENV_DIR"

if [[ -f "$BIN_TARGET" ]]; then
  ts="$(date +%Y%m%d-%H%M%S)"
  cp "$BIN_TARGET" "$BIN_TARGET.bak-$ts"
  echo "[OK] Backup бинарника: $BIN_TARGET.bak-$ts"
fi

if [[ -f "$DATA_DIR/nodax-central.db" ]]; then
  ts="$(date +%Y%m%d-%H%M%S)"
  cp "$DATA_DIR/nodax-central.db" "$BACKUP_DIR/nodax-central.db.$ts"
  echo "[OK] Backup БД: $BACKUP_DIR/nodax-central.db.$ts"
fi

echo "[2/10] Установка бинарника"
install -m 0755 "$BIN_SOURCE" "$BIN_TARGET"

if [[ ! -f "$ENV_FILE" ]]; then
  cat > "$ENV_FILE" <<EOF
NODAX_CENTRAL_PORT=$PORT
NODAX_DATA_DIR=$DATA_DIR
EOF
  echo "[OK] Создан env: $ENV_FILE"
else
  if grep -q '^NODAX_CENTRAL_PORT=' "$ENV_FILE"; then
    sed -i "s/^NODAX_CENTRAL_PORT=.*/NODAX_CENTRAL_PORT=$PORT/" "$ENV_FILE"
  else
    echo "NODAX_CENTRAL_PORT=$PORT" >> "$ENV_FILE"
  fi
  if grep -q '^NODAX_DATA_DIR=' "$ENV_FILE"; then
    sed -i "s|^NODAX_DATA_DIR=.*|NODAX_DATA_DIR=$DATA_DIR|" "$ENV_FILE"
  else
    echo "NODAX_DATA_DIR=$DATA_DIR" >> "$ENV_FILE"
  fi
fi

echo "[3/10] Настройка systemd"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=NODAX Central
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

chown -R "$RUN_USER":"$RUN_USER" "$DATA_DIR"

echo "[4/10] Перезагрузка systemd и запуск сервиса"
systemctl daemon-reload
systemctl enable "$APP_NAME" >/dev/null
systemctl restart "$APP_NAME"

sleep 2

echo "[5/10] Проверка сервиса"
systemctl is-active --quiet "$APP_NAME"
echo "[OK] Service $APP_NAME is running"

echo "[6/10] Smoke-check HTTP"
curl -fsS "http://127.0.0.1:$PORT/" >/dev/null
echo "[OK] HTTP доступен на 127.0.0.1:$PORT"

if [[ "$SETUP_CADDY" == "1" ]]; then
  echo "[7/10] Установка/проверка Caddy"
  if ! command -v caddy >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      apt-get update
      apt-get install -y caddy
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y caddy
    elif command -v yum >/dev/null 2>&1; then
      yum install -y caddy
    else
      echo "Не удалось установить Caddy автоматически: неподдерживаемый пакетный менеджер." >&2
      exit 1
    fi
  fi

  if [[ ! -f "$CADDY_TEMPLATE" ]]; then
    echo "Шаблон Caddy не найден: $CADDY_TEMPLATE" >&2
    exit 1
  fi

  echo "[8/10] Генерация Caddyfile"
  mkdir -p "$CADDY_CONFIG_DIR"
  if [[ -f "$CADDYFILE_PATH" ]]; then
    ts="$(date +%Y%m%d-%H%M%S)"
    cp "$CADDYFILE_PATH" "$CADDYFILE_PATH.bak-$ts"
    echo "[OK] Backup Caddyfile: $CADDYFILE_PATH.bak-$ts"
  fi
  sed \
    -e "s/central\\.example\\.com/$CADDY_DOMAIN/g" \
    -e "s/127\\.0\\.0\\.1:8080/127.0.0.1:$PORT/g" \
    "$CADDY_TEMPLATE" > "$CADDYFILE_PATH"

  echo "[9/10] Проверка и перезапуск Caddy"
  caddy validate --config "$CADDYFILE_PATH"
  systemctl enable caddy >/dev/null
  systemctl restart caddy
  systemctl is-active --quiet caddy
  echo "[OK] Caddy запущен, домен: $CADDY_DOMAIN"
else
  echo "[7/10] Настройка Caddy пропущена (SETUP_CADDY=0)"
fi

echo "[10/10] Краткий статус"
systemctl status "$APP_NAME" --no-pager -n 20 || true

echo "[DONE] Деплой завершён"
echo "URL: http://127.0.0.1:$PORT"
if [[ "$SETUP_CADDY" == "1" ]]; then
  echo "Public URL: https://$CADDY_DOMAIN"
fi
