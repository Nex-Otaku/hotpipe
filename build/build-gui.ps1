# Сборка окна hotpipe-gui (на Windows).
#
#   build\build-gui.ps1
#   build\build-gui.ps1 -Bundle     # ещё и установщик
#
# Cargo не работает в каталоге на сетевом пути (блокировки файлов там не
# поддерживаются), а проект живёт в WSL и виден Windows как \\wsl.localhost\...
# Поэтому сборка идёт через местную копию в cache\gui-build. Имя каталога
# постоянное: сборочный кэш Rust в нём — больше гигабайта, и со случайным
# именем он не переиспользовался бы, а копился (урок wsldrive).

param([switch]$Bundle)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root "build"
$gui = Join-Path $root "src\gui"

. (Join-Path $PSScriptRoot "paths.ps1")
. (Join-Path $PSScriptRoot "version.ps1")

$v = Get-HotpipeVersion -Root $root
$env:HOTPIPE_VERSION = $v.Number
$env:HOTPIPE_COMMIT = $v.Commit
$env:HOTPIPE_BUILT = $v.Built
$env:HOTPIPE_DIRTY = $v.Dirty
Use-HotpipeTemp
Write-Host "версия $($v.Number), снимок $($v.Commit), изменённое дерево: $($v.Dirty)"

$src = $gui
if ($src.StartsWith("\\")) {
    $tmp = Join-Path $HotpipeCache "gui-build"
    Write-Host "исходный код на сетевом пути, собираю через $tmp"
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null
    # Кэш (src-tauri\target) сохраняем, исходный код заводим заново: удалённый
    # в хранилище файл не должен задерживаться в копии и собираться дальше.
    Get-ChildItem -Force $tmp | Where-Object { $_.Name -ne "src-tauri" } | Remove-Item -Recurse -Force
    $tauriDir = Join-Path $tmp "src-tauri"
    if (Test-Path $tauriDir) {
        Get-ChildItem -Force $tauriDir | Where-Object { $_.Name -ne "target" } | Remove-Item -Recurse -Force
    }
    Copy-Item -Recurse -Force -Path (Join-Path $gui "*") -Destination $tmp
    $src = $tmp
}

if (-not (Test-Path $out)) { New-Item -ItemType Directory -Path $out | Out-Null }

Push-Location (Join-Path $src "src-tauri")
try {
    cargo test --release
    if ($LASTEXITCODE -ne 0) { throw "проверки окна не прошли" }
    if ($Bundle) {
        cargo tauri build
    } else {
        cargo build --release --features custom-protocol
    }
    if ($LASTEXITCODE -ne 0) { throw "сборка окна не удалась" }
    $exe = Join-Path $src "src-tauri\target\release\hotpipe-gui.exe"
    if (-not (Test-Path $exe)) { throw "собранный файл не найден: $exe" }
    Copy-Item -Force $exe (Join-Path $out "hotpipe-gui.exe")
} finally {
    Pop-Location
}

Write-Host "собрано: $(Join-Path $out 'hotpipe-gui.exe') версии $($v.Number)"
