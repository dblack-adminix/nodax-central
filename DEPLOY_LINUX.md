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
chmod +x ./deploy-linux.sh
sudo pwsh -File ./install-central.ps1
```

По умолчанию скрипт берёт бинарник `./nodax-central`, ставит сервис `nodax-central`, порт `8080`.

> Примечание: для единой точки входа нужен `pwsh` (PowerShell 7+) на Linux.

## 4. Параметры скрипта

Можно переопределить через переменные окружения (они будут переданы в `deploy-linux.sh`):

```bash
sudo APP_NAME=nodax-central PORT=8080 BIN_SOURCE=./nodax-central pwsh -File ./install-central.ps1
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

1) Установите Caddy (Ubuntu/Debian):

```bash
sudo apt update
sudo apt install -y caddy
```

2) Скопируйте шаблон конфига и укажите ваш домен:

```bash
sudo cp ./Caddyfile.linux /etc/caddy/Caddyfile
```

Файл-шаблон находится в пакете: `deploy/Caddyfile.linux`.

3) Проверьте и перезапустите Caddy:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl restart caddy
sudo systemctl status caddy --no-pager
```

После этого Caddy автоматически поднимет HTTPS и будет проксировать на `127.0.0.1:8080`.

## 8. Rollback (ручной)

- Восстановите бинарник из `/opt/nodax-central/bin/*.bak-*`
- Восстановите БД из `/var/lib/nodax-central/backups/`
- Выполните `systemctl restart nodax-central`
