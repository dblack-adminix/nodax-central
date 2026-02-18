# Деплой NODAX Central на Windows

Документ описывает production-развёртывание `nodax-central.exe` как службы Windows.

## 1. Требования

- Windows Server / Windows 10+
- PowerShell от имени администратора
- Готовый `nodax-central.exe` (frontend встроен)

## 2. Целевая структура

- `C:\Program Files\NodaxCentral\nodax-central.exe` — бинарник
- `C:\ProgramData\NodaxCentral` — рабочая папка и БД
- `C:\ProgramData\NodaxCentral\backups` — резервные копии БД

## 3. Быстрый деплой (рекомендуется)

1) Скопируйте на сервер:
- `nodax-central.exe`
- `deploy\install-central.ps1`
- `deploy\deploy-windows.ps1`

2) Запустите в PowerShell (Администратор):

```powershell
powershell -ExecutionPolicy Bypass -File .\install-central.ps1
```

Чтобы сразу настроить Caddy (proxy + TLS), запускайте так:

```powershell
powershell -ExecutionPolicy Bypass -File .\install-central.ps1 -SetupCaddy -CaddyDomain "central.example.com"
```

Либо отдельным инсталлером Caddy:

```powershell
powershell -ExecutionPolicy Bypass -File .\install-caddy.ps1 -Domain "central.example.com" -UpstreamPort 8080
```

## 4. Параметры скрипта

```powershell
powershell -ExecutionPolicy Bypass -File .\install-central.ps1 `
  -ServiceName NODAXCentral `
  -InstallDir "C:\Program Files\NodaxCentral" `
  -DataDir "C:\ProgramData\NodaxCentral" `
  -Port 8080 `
  -BinarySource ".\nodax-central.exe" `
  -SetupCaddy `
  -CaddyDomain "central.example.com" `
  -CaddyBinarySource ".\caddy.exe"
```

## 5. Проверка

```powershell
Get-Service NODAXCentral
Invoke-WebRequest http://127.0.0.1:8080/ -UseBasicParsing
```

## 6. Обновление

Повторно запускайте тот же скрипт с новой версией бинарника.
Скрипт автоматически:
- останавливает службу
- делает backup БД (если найдена)
- обновляет бинарник
- запускает службу
- выполняет smoke-check HTTP

## 7. Reverse proxy + TLS (Caddy, рекомендовано)

Для Windows рекомендуем Caddy как основной proxy: проще конфиг и авто HTTPS.

1) Установите Caddy (через winget):

```powershell
winget install --id CaddyServer.Caddy -e
```

2) Скопируйте шаблон конфига и замените домен:

```powershell
Copy-Item .\Caddyfile.windows "C:\ProgramData\Caddy\Caddyfile" -Force
```

Файл-шаблон находится в пакете: `deploy/Caddyfile.windows`.

3) Запустите Caddy как службу:

```powershell
sc.exe create Caddy binPath= "\"C:\Program Files\Caddy\caddy.exe\" run --environ --config \"C:\ProgramData\Caddy\Caddyfile\"" start= auto
Start-Service Caddy
Get-Service Caddy
```

После этого Caddy будет принимать HTTPS на `443` и проксировать на `127.0.0.1:8080`.

## 8. Rollback (ручной)

- Восстановите прошлый `nodax-central.exe` из `InstallDir`
- Восстановите БД из `DataDir\backups`
- Перезапустите службу `NODAXCentral`
