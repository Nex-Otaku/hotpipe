#!/usr/bin/env bash
# Кладёт собранный hotpipe-server в ~/.local/bin и перезапускает работающий.
#
# Отдельно от build/: окно запускает сервер по постоянному пути, и пересборка
# не должна подменять файл под работающим процессом. install кладёт новый файл
# рядом и переименовывает — работающий процесс доживает на старом.
#
# Работающий сервер просим завершиться: сторож окна (hotpipe-gui --keeper)
# поднимет его снова уже из нового файла. Без сторожа сервер придётся поднять
# руками или окном.
#
#   build/install.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/build/hotpipe-server"
DST="$HOME/.local/bin/hotpipe-server"
PORT="${HOTPIPE_PORT:-46310}"

[[ -x "$SRC" ]] || { echo "нет $SRC — сначала build/build.sh" >&2; exit 1; }
mkdir -p "$(dirname "$DST")"
install -m 755 "$SRC" "$DST"
echo "установлено: $DST"
"$DST" -version

if curl -s --max-time 2 -o /dev/null "http://127.0.0.1:$PORT/api/status"; then
	echo "сервер работает — прошу перезапуститься на новом файле"
	curl -s --max-time 3 -X POST -H 'X-Hotpipe: 1' "http://127.0.0.1:$PORT/api/shutdown" >/dev/null || true
fi
