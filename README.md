# NODAX Central

Центральная веб-панель для управления несколькими Hyper-V хостами через агенты (nodax-server).

---

## Архитектура

```
┌─────────────────────┐
│   NODAX Central     │  ← Веб-панель (Go + React)
│   localhost:8080    │
└────────┬────────────┘
         │ HTTP polling (каждые 15 сек)
    ┌────┴─────┬──────────┐
    ▼          ▼          ▼
┌─────────┐ ┌─────────┐ ┌─────────┐
│ Agent 1 │ │ Agent 2 │ │ Agent N │  ← nodax-server на каждом хосте
│ :9000   │ │ :9000   │ │ :9000   │
└─────────┘ └─────────┘ └─────────┘
  Hyper-V     Hyper-V     Hyper-V
```

- **NODAX Central** — единый дашборд, Go HTTP сервер с встроенным React фронтендом
- **Агенты** — существующие nodax-server инстансы на каждом Hyper-V хосте
- Центральный сервер опрашивает агентов по их REST API (status, VMs, host info, health)
- Все команды проксируются через центральный сервер к нужному агенту

## Возможности

- Регистрация Hyper-V хостов (имя, URL, API ключ)
- Автоматический опрос всех хостов каждые 15 секунд
- Обзорный дашборд: хосты онлайн, ВМ всего/запущено, CPU/RAM
- Детальная страница хоста: метрики, Health Check, список ВМ
- Проксирование API запросов к агентам
- Единый бинарник (Go + встроенный React)

## Требования

- **Go 1.24+**
- **Node.js 18+**

## Сборка

```powershell
# Установка frontend зависимостей
cd frontend && npm install && cd ..

# Сборка frontend
cd frontend && npx tsc && npx vite build && cd ..

# Сборка Go бинарника (встраивает frontend/dist)
go mod tidy
go build -o nodax-central.exe .
```

## Запуск

```powershell
# По умолчанию порт 8080
.\nodax-central.exe

# Или с кастомным портом
$env:NODAX_CENTRAL_PORT = "3000"
.\nodax-central.exe
```

Откройте `http://localhost:8080` в браузере.

## Лицензирование Central (hybrid)

- В `Настройки` задаются:
  - `licenseKey`
  - `licenseServer` (URL License Server)
  - `licensePubKey` (резерв под проверку подписи)
- Central делает авто-проверку лицензии при старте и далее каждые 12 часов.
- При недоступности лиценз-сервера действует `grace` (если ранее был валидный ответ).
- При невалидной лицензии блокируются write-операции API (кроме `/api/config` и `/api/license/recheck`).

ENV:

- `NODAX_LICENSE_SERVER` — дефолтный URL License Server (если пусто в config)

## Деплой в production

- Единая точка входа: `deploy/install-central.ps1`
- Linux (systemd): `DEPLOY_LINUX.md`
- Windows (Service): `DEPLOY_WINDOWS.md`
- Caddy templates:
  - `deploy/Caddyfile.linux`
  - `deploy/Caddyfile.windows`
- Скрипты автоматизации:
  - `deploy/install-central.ps1`
  - `deploy/deploy-linux.sh`
  - `deploy/deploy-windows.ps1`

## Использование

1. Нажмите **"Добавить хост"** в боковой панели
2. Введите:
   - **Имя** — произвольное отображаемое имя (например "HV-SERVER-01")
   - **URL** — адрес nodax-server (например `http://192.168.1.10:9000`)
   - **API Key** — ключ авторизации (из настроек nodax)
3. Хост появится в боковой панели, статус обновится автоматически
4. Кликните на хост для просмотра деталей

## REST API

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/agents` | Список всех хостов |
| POST | `/api/agents` | Добавить хост (`{name, url, apiKey}`) |
| GET | `/api/agents/{id}` | Информация о хосте |
| PUT | `/api/agents/{id}` | Обновить хост |
| DELETE | `/api/agents/{id}` | Удалить хост |
| GET | `/api/overview` | Агрегированные метрики всех хостов |
| GET | `/api/agents/{id}/data` | Кэшированные данные хоста (VMs, health, host info) |
| ANY | `/api/agents/{id}/proxy/...` | Проксирование запроса к агенту |
| GET | `/api/license/status` | Текущий статус лицензии Central |
| POST | `/api/license/recheck` | Принудительная повторная проверка лицензии |

### Пример проксирования

Запустить ВМ на конкретном хосте:
```
POST /api/agents/{id}/proxy/api/v1/vm/DC-01/action
Content-Type: application/json
{"action": "start"}
```

## Структура проекта

```
nodax-central/
├── main.go                        # Entry point, HTTP server, embed frontend
├── go.mod / go.sum
├── internal/
│   ├── models/models.go           # Agent, AgentData, HostInfo, VM, HealthReport
│   ├── store/store.go             # BoltDB: хранение агентов
│   ├── poller/poller.go           # Фоновый опрос агентов
│   └── api/handlers.go            # HTTP handlers (CRUD + proxy)
├── frontend/
│   ├── src/
│   │   ├── App.tsx                # React дашборд
│   │   ├── App.css                # Стили (dark theme)
│   │   └── main.tsx               # Entry point
│   ├── index.html
│   ├── package.json
│   ├── vite.config.ts
│   └── tsconfig.json
└── nodax-central.db               # BoltDB (создаётся автоматически)
```

## Лицензия

MIT License
