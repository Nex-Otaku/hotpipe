# Где проект держит свои файлы на Windows.
#
# Не на системном диске (как в wsldrive, Р-049 там): всё, что проект кладёт на
# Windows, — окно, его настройки и журнал, данные WebView2, сборочные кэши —
# лежит в одном каталоге. Убрать проект — снести один каталог.
#
#   D:\Dev\projects\hotpipe          hotpipe-gui.exe, gui-config.json, журнал
#   D:\Dev\projects\hotpipe\cache    сборочные кэши; сносится без последствий
#
# Другое место задаётся переменной окружения HOTPIPE_HOME.

$HotpipeHome = if ($env:HOTPIPE_HOME) { $env:HOTPIPE_HOME } else { "D:\Dev\projects\hotpipe" }
$HotpipeCache = Join-Path $HotpipeHome "cache"

# Временные файлы сборки — туда же: компоновщик пишет их в %TEMP%, то есть на C:.
function Use-HotpipeTemp {
    $t = Join-Path $HotpipeCache "tmp"
    New-Item -ItemType Directory -Force -Path $t | Out-Null
    $env:TEMP = $t
    $env:TMP = $t
}
