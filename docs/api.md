# HTTP API

`127.0.0.1:46310`, только петлевой адрес. Изменяющие запросы требуют заголовка
`X-Hotpipe` — иначе до API могла бы дотянуться любая страница в браузере
([Р-006](decisions.md)).

```bash
curl http://127.0.0.1:46310/                       # сводка для человека
curl http://127.0.0.1:46310/api/status             # всё состояние, JSON
curl http://127.0.0.1:46310/api/log                # хвост журнала
H='X-Hotpipe: 1'
curl -X POST   -H "$H" -d '{"name":"pg","host":"h","user":"u","local_port":5432,"remote_port":5432,"enabled":true}' \
                                    http://127.0.0.1:46310/api/tunnels
curl -X PUT    -H "$H" -d '{…}'     http://127.0.0.1:46310/api/tunnels/<id>
curl -X DELETE -H "$H"              http://127.0.0.1:46310/api/tunnels/<id>
curl -X POST   -H "$H"              http://127.0.0.1:46310/api/tunnels/<id>/start   # stop, restart
curl -X PUT    -H "$H" -d '{"server_alive_interval":5,"server_alive_count_max":3}' \
                                    http://127.0.0.1:46310/api/settings
curl -X POST   -H "$H"              http://127.0.0.1:46310/api/retry      # все ждущие — сейчас
curl -X POST   -H "$H"              http://127.0.0.1:46310/api/reload     # перечитать файл
curl -X POST   -H "$H"              http://127.0.0.1:46310/api/shutdown
```
