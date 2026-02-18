# License Server (MVP)

Отдельный сервис лицензирования для `nodax-central` (hybrid-модель).

## Что умеет

- Создать лицензию (клиент, план, лимит хостов, срок)
- Получить список лицензий
- Продлить срок лицензии
- Отозвать лицензию (revoke)
- Проверить лицензию онлайн (`/api/v1/license/validate`)
- Подписать ответ проверки (Ed25519)
- Встроенная web-админка управления лицензиями: `/admin`

## Запуск

```powershell
# из корня проекта
$env:LICENSE_ADMIN_TOKEN = "super-secret-token"
$env:LICENSE_SERVER_PORT = "8091"
go run .\license-server
```

По умолчанию:
- порт: `8091`
- БД: `license-server.db` рядом с бинарником
- ключ подписи: `license-sign.key` рядом с бинарником
- grace: `7` дней

После запуска откройте:

- `http://127.0.0.1:8091/admin` — UI управления лицензиями

### Полезные ENV

- `LICENSE_ADMIN_TOKEN` — токен для админ API (обязательно в проде)
- `LICENSE_SERVER_PORT` — порт HTTP сервера
- `LICENSE_DB_PATH` — путь к файлу БД
- `LICENSE_SIGN_KEY_PATH` — путь к приватному ключу подписи
- `LICENSE_DATA_DIR` — директория хранения данных (БД, ключ подписи)
- `LICENSE_GRACE_DAYS` — количество grace дней для central

## Прод деплой (Debian 13 + Caddy)

В репозитории есть готовый скрипт:

`deploy/deploy-license-server-debian.sh`

Что делает:
- ставит/проверяет Caddy
- разворачивает бинарник `license-server` в `/opt/nodax-license-server/bin`
- создает env в `/etc/nodax-license-server/license-server.env`
- поднимает `systemd` сервис `nodax-license-server`
- настраивает reverse proxy в `Caddyfile` для вашего домена
- запускает smoke-check `/healthz`

### 1) Собрать Linux-бинарник

На Linux-хосте:

```bash
cd /path/to/nodax-central
go build -o license-server ./license-server
chmod +x ./license-server
```

### 2) Запустить деплой

```bash
cd /path/to/nodax-central
sudo LICENSE_DOMAIN=license.example.com \
     LICENSE_ADMIN_TOKEN='your-strong-token' \
     ./deploy/deploy-license-server-debian.sh
```

Если `LICENSE_ADMIN_TOKEN` не передать, скрипт сгенерирует его автоматически и выведет в консоль.

### 3) Проверка

```bash
systemctl status nodax-license-server --no-pager
systemctl status caddy --no-pager
curl -fsS http://127.0.0.1:8091/healthz
```

Публичные URL:
- `https://license.example.com/admin`
- `https://license.example.com/client`

## API

### 1) Создать лицензию (admin)

`POST /api/v1/licenses`

Headers:
- `Authorization: Bearer <LICENSE_ADMIN_TOKEN>`
- `Content-Type: application/json`

Body:
```json
{
  "customerName": "ООО Ромашка",
  "plan": "pro",
  "maxAgents": 25,
  "validDays": 365,
  "notes": "годовая подписка"
}
```

### 2) Список лицензий (admin)

`GET /api/v1/licenses`

### 3) Продлить лицензию (admin)

`POST /api/v1/licenses/{id}/extend`

Body (вариант 1):
```json
{ "days": 30 }
```

Body (вариант 2):
```json
{ "expiresAt": "2027-12-31T23:59:59Z" }
```

### 4) Отозвать лицензию (admin)

`POST /api/v1/licenses/{id}/revoke`

### 5) Проверить лицензию (public)

`POST /api/v1/license/validate`

Body:
```json
{
  "licenseKey": "NDX-ABCDEF-123456-A1B2C3-FFEEDD",
  "instanceId": "central-001",
  "hostname": "central-prod",
  "version": "1.0.0",
  "agentCount": 12
}
```

Ответ:
```json
{
  "payload": {
    "licenseId": "...",
    "status": "active",
    "valid": true,
    "plan": "pro",
    "maxAgents": 25,
    "expiresAt": "2027-02-17T18:00:00Z",
    "graceDays": 7,
    "serverTime": "2026-02-17T18:00:00Z"
  },
  "signature": "base64...",
  "algorithm": "ed25519"
}
```

### 6) Публичный ключ

`GET /api/v1/public-key`

Используется central для проверки подписи ответа `validate`.

## Быстрый smoke test (PowerShell)

```powershell
$token = "super-secret-token"

# create
$created = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8091/api/v1/licenses" `
  -Headers @{ Authorization = "Bearer $token" } `
  -ContentType "application/json" `
  -Body (@{ customerName="Test LLC"; plan="pro"; maxAgents=10; validDays=30 } | ConvertTo-Json)

# validate
Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8091/api/v1/license/validate" `
  -ContentType "application/json" `
  -Body (@{ licenseKey=$created.licenseKey; instanceId="central-01"; agentCount=3 } | ConvertTo-Json)
```
