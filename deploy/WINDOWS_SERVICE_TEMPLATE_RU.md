# Шаблон службы NODAX Central для Windows

Ниже пример команд для регистрации службы через `sc.exe`.

```powershell
$ServiceName = "NODAXCentral"
$DisplayName = "NODAX Central"
$BinaryPath = "C:\Program Files\NodaxCentral\nodax-central.exe"

sc.exe create $ServiceName binPath= "`"$BinaryPath`"" start= auto DisplayName= "`"$DisplayName`""
sc.exe description $ServiceName "`"NODAX Central service`""
sc.exe failure $ServiceName reset= 86400 actions= restart/60000/restart/60000/restart/60000
Start-Service $ServiceName
```

Проверка:

```powershell
Get-Service NODAXCentral
Invoke-WebRequest http://127.0.0.1:8080/ -UseBasicParsing
```
