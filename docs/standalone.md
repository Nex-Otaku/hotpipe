# Без окна

Сервер — обычная программа Linux и работает и сам:

```bash
hotpipe-server                    # ~/.config/hotpipe/tunnels.json, API на 127.0.0.1:46310
hotpipe-server -config x.json -listen 127.0.0.1:46400 -log /tmp/h.log
hotpipe-server -version
```

Коды выхода: 0 — остановлен по просьбе, 1 — не запустился, 3 — порт API занят
(сервер уже работает).
