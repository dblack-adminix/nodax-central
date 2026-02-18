# Деплой NODAX Central на Linux

Документ описывает production-развёртывание `nodax-central` как `systemd`-сервиса с хранением данных отдельно от бинарника.

## 1. Требования

- Ubuntu/Debian/CentOS с `systemd`
- root-доступ
- Пакеты: `curl`, `tar` (опционально `nginx` для reverse proxy)
- Готовый бинарник `nodax-central` (frontend уже встроен)

## 2. Целевая структура

- `/opt/nodax-central/bin/nodax-central` — бинарник
- `/var/lib/nodax-central` — рабочая директория и БД
- `/var/lib/nodax-central/backups` — резервные копии
- `/etc/nodax-central/nodax-central.env` — переменные окружения
- `nodax-central.service` — systemd unit

## 3. Быстрый деплой (рекомендуется)

1) Скопируйте на сервер:
- `nodax-central` (бинарник)
- `deploy/deploy-linux.sh`
- `deploy/install-central.ps1`

2) Запустите:

```bash
chmod +x ./deploy/deploy-linux.sh
sudo pwsh -File ./deploy/install-central.ps1
```

По умолчанию скрипт берёт бинарник `./nodax-central`, ставит сервис `nodax-central`, порт `8080`.

> Примечание: для единой точки входа нужен `pwsh` (PowerShell 7+) на Linux.

## 4. Параметры скрипта

Можно переопределить через переменные окружения (они будут переданы в `deploy-linux.sh`):

```bash
sudo APP_NAME=nodax-central PORT=8080 BIN_SOURCE=./nodax-central pwsh -File ./install-central.ps1
```

## 4.1 Деплой из Git (Debian 13)

Если вы клонируете репозиторий, сначала соберите Linux-бинарник:

```bash
git clone -b main https://github.com/dblack-adminix/nodax-central.git /opt/nodax-central
cd /opt/nodax-central

sudo apt update
sudo apt install -y golang-go curl

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o nodax-central .
chmod +x ./deploy/deploy-linux.sh
sudo BIN_SOURCE=./nodax-central ./deploy/deploy-linux.sh
```

## 5. Проверка

```bash
systemctl status nodax-central --no-pager
curl -I http://127.0.0.1:8080/
```

## 6. Обновление

Повторно запустите тот же `deploy-linux.sh` с новым бинарником.
Скрипт автоматически:
- делает backup текущего бинарника
- делает backup БД (если найдена)
- перезапускает сервис

## 7. Reverse proxy + TLS (Caddy, рекомендовано)

Используем Caddy как основной reverse proxy для Linux.

### 7.1 Автоматически через deploy-linux.sh (рекомендуется)

```bash
sudo SETUP_CADDY=1 CADDY_DOMAIN=central.example.com BIN_SOURCE=./nodax-central ./deploy/deploy-linux.sh
```

Что делает автоматизация:
- устанавливает `caddy` (apt/dnf/yum, если отсутствует)
- генерирует `/etc/caddy/Caddyfile` из `deploy/Caddyfile.linux`
- подставляет ваш домен и локальный порт Central
- валидирует конфиг и перезапускает `caddy`

Также можно через единый entrypoint:

```bash
sudo pwsh -File ./deploy/install-central.ps1 -BinarySource ./nodax-central -SetupCaddy -CaddyDomain central.example.com
```

### 7.2 Ручная настройка (если нужно)

```bash
sudo apt update
sudo apt install -y caddy
sudo cp ./deploy/Caddyfile.linux /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl restart caddy
sudo systemctl status caddy --no-pager
```

После этого Caddy автоматически поднимет HTTPS и будет проксировать на `127.0.0.1:8080`.

## 8. Rollback (ручной)

- Восстановите бинарник из `/opt/nodax-central/bin/*.bak-*`
- Восстановите БД из `/var/lib/nodax-central/backups/`
- Выполните `systemctl restart nodax-central`
