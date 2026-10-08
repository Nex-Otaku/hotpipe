# Раскладывает окно на местный диск.
#
# Путь \\wsl.localhost\... Windows относит к зоне «Интернет», и запуск оттуда
# каждый раз упирается в вопрос о непроверенном издателе. С местного диска —
# нет (урок wsldrive, Р-045 там). Да и автозапуск должен вести к файлу, который
# доступен до того, как поднялась WSL.
#
# Сервер в WSL ставится отдельно, из WSL: build/install.sh.
#
#   build\deploy.ps1
#   build\deploy.ps1 -To E:\hotpipe
#   build\deploy.ps1 -Start

param([string]$To = "", [switch]$Start)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "paths.ps1")
if ($To -eq "") { $To = $HotpipeHome }
if ($To.StartsWith("\\")) { throw "каталог $To — сетевой путь, а разложить надо на местный диск" }
if (-not (Test-Path $To)) { New-Item -ItemType Directory -Path $To | Out-Null }

$src = Join-Path (Split-Path -Parent $PSScriptRoot) "build\hotpipe-gui.exe"
if (-not (Test-Path $src)) { throw "нет $src — сначала build\build-gui.ps1" }

$dst = Join-Path $To "hotpipe-gui.exe"
# Работающий файл Windows перезаписать не даёт. Это может быть окно или сторож
# (hotpipe-gui.exe --keeper): сторожа можно остановить — туннели в WSL при этом
# падают, пока он не будет запущен снова.
try {
    Copy-Item -Force $src $dst -ErrorAction Stop
} catch {
    Write-Host ""
    Write-Host "НЕ ЗАМЕНИТЬ $dst — файл занят окном или сторожем." -ForegroundColor Red
    Write-Host "Закройте окно и остановите сторожа:"
    Write-Host "  Stop-Process -Name hotpipe-gui"
    Write-Host "затем повторите раскладку (окно при запуске поставит сторожа снова)."
    exit 1
}
Unblock-File -Path $dst -ErrorAction SilentlyContinue
Write-Host "разложено: $dst"

if ($Start) { Start-Process $dst }
