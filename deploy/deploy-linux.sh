#!/usr/bin/env bash
set -euo pipefail

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

if [[ "$EUID" -ne 0 ]]; then
  echo "Запустите скрипт от root (sudo)." >&2
  exit 1
fi

if [[ ! -f "$BIN_SOURCE" ]]; then
  echo "Бинарник не найден: $BIN_SOURCE" >&2
  exit 1
fi

echo "[1/8] Подготовка пользователя и директорий"
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

echo "[2/8] Установка бинарника"
install -m 0755 "$BIN_SOURCE" "$BIN_TARGET"

if [[ ! -f "$ENV_FILE" ]]; then
  cat > "$ENV_FILE" <<EOF
NODAX_CENTRAL_PORT=$PORT
EOF
  echo "[OK] Создан env: $ENV_FILE"
else
  if grep -q '^NODAX_CENTRAL_PORT=' "$ENV_FILE"; then
    sed -i "s/^NODAX_CENTRAL_PORT=.*/NODAX_CENTRAL_PORT=$PORT/" "$ENV_FILE"
  else
    echo "NODAX_CENTRAL_PORT=$PORT" >> "$ENV_FILE"
  fi
fi

echo "[3/8] Настройка systemd"
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

echo "[4/8] Перезагрузка systemd и запуск сервиса"
systemctl daemon-reload
systemctl enable "$APP_NAME" >/dev/null
systemctl restart "$APP_NAME"

sleep 2

echo "[5/8] Проверка сервиса"
systemctl is-active --quiet "$APP_NAME"
echo "[OK] Service $APP_NAME is running"

echo "[6/8] Smoke-check HTTP"
curl -fsS "http://127.0.0.1:$PORT/" >/dev/null
echo "[OK] HTTP доступен на 127.0.0.1:$PORT"

echo "[7/8] Краткий статус"
systemctl status "$APP_NAME" --no-pager -n 20 || true

echo "[8/8] Деплой завершён"
echo "URL: http://127.0.0.1:$PORT"
