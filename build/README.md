# build — сборка

Результаты сборки сюда же, в хранилище они не попадают (перечислены в
`.gitignore` поимённо).

| Сценарий | Где запускать | Что делает |
|---|---|---|
| `build.sh` | WSL | проверки и сборка `hotpipe-server` |
| `install.sh` | WSL | кладёт сервер в `~/.local/bin`, просит работающий перезапуститься (сторож поднимет новый) |
| `build-gui.ps1` | Windows | проверки и сборка `hotpipe-gui.exe` через копию в `D:\Dev\projects\hotpipe\cache\gui-build` |
| `deploy.ps1` | Windows | кладёт окно в `D:\Dev\projects\hotpipe`; `-Start` — и запускает |
| `build-macos.sh` | macOS | сервер и окно в одном `HotPipe.app`; `-install` — в `~/Applications` |
| `paths.ps1`, `version.ps1` | — | общие части сценариев Windows |

Из WSL сценарии Windows запускаются так:

```bash
# из корня проекта
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$(wslpath -w build/build-gui.ps1)"
```

`deploy.ps1` не заменит файл, пока работают окно или сторож: `Stop-Process -Name
hotpipe-gui` (туннели упадут до запуска окна — оно поставит сторожа снова).

Сценарии PowerShell хранятся с меткой BOM — без неё PowerShell 5.1 читает
кириллицу как ANSI и перестаёт разбирать файл ([Р-009](../docs/decisions.md)).
