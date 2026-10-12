# Файлы

| Что | Где |
|---|---|
| туннели и настройки ssh | WSL: `~/.config/hotpipe/tunnels.json` (+ `.bak` — прежняя версия) |
| журнал сервера | WSL: `~/.config/hotpipe/hotpipe-server.log` (16 МиБ, затем `.log.1`) |
| окно, его настройки, журнал окна и сторожа | `D:\Dev\projects\hotpipe\` — `gui-config.json`, `hotpipe-gui.log` |
| данные WebView2 | `D:\Dev\projects\hotpipe\webview\` |
| сборочные кэши | `D:\Dev\projects\hotpipe\cache\` — сносится без последствий |

`tunnels.json` можно править руками; после правки — `kill -HUP $(pgrep -x hotpipe-server)`
или `curl -X POST -H 'X-Hotpipe: 1' http://127.0.0.1:46310/api/reload`.

```json
{
  "settings": { "server_alive_interval": 5, "server_alive_count_max": 3 },
  "tunnels": [
    {
      "id": "3f1a9c2e", "name": "MySQL db.example.com", "enabled": true,
      "host": "db.example.com", "port": 2222, "user": "deploy",
      "local_host": "localhost", "local_port": 5001,
      "remote_host": "127.0.0.1", "remote_port": 3306
    }
  ]
}
```

`local_host: "localhost"` — как у `ssh -L 5001:…`: слушаются 127.0.0.1 и ::1.
Порты WSL на петлевом адресе Windows видит сама, так что туннель доступен и из
Windows по `127.0.0.1:5001`.
