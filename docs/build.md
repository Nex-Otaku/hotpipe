# Сборка

```
src/
  cmd/hotpipe-server   main: флаги, журнал, сигналы, порт API
  internal/config      tunnels.json: туннели, проверка полей, запись через переименование
  internal/tunnel      надзор за ssh: аргументы, /proc, перевод ошибок ssh, повторы
  internal/api         HTTP API
  internal/logfile     журнал в файле (из wsldrive)
  internal/version     сведения о сборке (из wsldrive)
  gui/                 окно на Tauri 1: Rust + Alpine, Tailwind, Flowbite
    src-tauri/src      main.rs — команды; server.rs — сторож и wsl.exe;
                       autostart.rs — реестр; http.rs; config.rs; applog.rs
    ui/                index.html, app.js (в т.ч. разбор вставленной команды ssh)
build/                 build.sh, install.sh (WSL); build-gui.ps1, deploy.ps1 (Windows)
dist/                  выпуски и журнал изменений
docs/                  документация и журнал решений
tools/gui/             shot.ps1 — снимок окна, uia.ps1 — нажатия в окне (проверки);
                       demo/ — выдуманные туннели для снимка в README
```

Проверки: `cd src && go test -race ./...` (сервер, в т.ч. сквозные с поддельным
ssh: обрыв, отказ в ключе, чужой и осиротевший порт) и `cargo test` в
`src/gui/src-tauri` (их запускает `build-gui.ps1`).

Живая проверка — настоящий ssh до своего сервера — включается файлом `.env` в
корне проекта: скопируйте `.env.example` и впишите свой сервер. `.env` в
хранилище не попадает. Без него проверка пропускается, `build.sh` её не
запускает никогда.

```bash
cp .env.example .env    # и вписать свой сервер
cd src && go test -run TestLiveTunnel -v ./internal/tunnel/
```

Снимок окна для README (`docs/images/window.png`) снят с выдуманными туннелями
из `tools/gui/demo`: копия `hotpipe-gui.exe` в отдельной папке, рядом
`gui-config.json` с `"api_port": 46399` и `"server_binary"`, указывающим на
`tools/gui/demo/demo-server`. Поддельный ssh держит порты 5435 и 6379 — они
должны быть свободны. Снимок — `tools/gui/shot.ps1 -ProcessId <номер окна>`.
