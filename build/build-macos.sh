#!/usr/bin/env bash
# Сборка HotPipe.app для macOS: окно и сервер в одном пакете (Р-010).
#
# Сервер собирается build/build.sh (с его проверками) и кладётся в пакет
# рядом с окном: Contents/MacOS/hotpipe-server. Окно по умолчанию запускает
# именно его — ставить сервер отдельно в ~/.local/bin не нужно.
#
# Нужны Go, Rust и tauri-cli первой версии:
#   brew install go rustup && rustup default stable
#   cargo install tauri-cli --version "^1" --locked
#
#   build/build-macos.sh              собрать build/HotPipe.app
#   build/build-macos.sh -install     и положить в ~/Applications (работающий
#                                     сервер перезапустится уже из нового пакета)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/build"
TAURI="$ROOT/src/gui/src-tauri"
PORT="${HOTPIPE_PORT:-46310}"

INSTALL=no
[[ "${1:-}" == "-install" ]] && INSTALL=yes

[[ "$(uname -s)" == "Darwin" ]] || { echo "этот сценарий — для macOS" >&2; exit 1; }
# rustup из Homebrew в PATH сам не попадает.
[[ -d /opt/homebrew/opt/rustup/bin ]] && PATH="/opt/homebrew/opt/rustup/bin:$PATH"
[[ -d "$HOME/.cargo/bin" ]] && PATH="$HOME/.cargo/bin:$PATH"

# Сервер — тем же сценарием, что и в WSL: версия и проверки те же.
"$ROOT/build/build.sh"

# Версия — так же, как в build.sh: окно и сервер должны совпадать.
if git -C "$ROOT" rev-parse HEAD >/dev/null 2>&1; then
	export HOTPIPE_VERSION="0.1.$(git -C "$ROOT" rev-list --count HEAD)-alpha"
	export HOTPIPE_COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
	if git -C "$ROOT" diff --quiet HEAD 2>/dev/null; then export HOTPIPE_DIRTY=no; else export HOTPIPE_DIRTY=yes; fi
fi
export HOTPIPE_BUILT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Tauri ищет сопутствующую программу с суффиксом целевой платформы и кладёт
# её в пакет уже без суффикса.
TRIPLE="$(rustc -vV | sed -n 's/^host: //p')"
mkdir -p "$TAURI/binaries"
cp "$OUT/hotpipe-server" "$TAURI/binaries/hotpipe-server-$TRIPLE"

# icon.icns — из icon.png (его рисует icons/make-icon.py).
ICONSET="$(mktemp -d)/icon.iconset"
mkdir -p "$ICONSET"
for z in 16 32 128 256 512; do
	sips -z $z $z "$TAURI/icons/icon.png" --out "$ICONSET/icon_${z}x${z}.png" >/dev/null
	sips -z $((z * 2)) $((z * 2)) "$TAURI/icons/icon.png" --out "$ICONSET/icon_${z}x${z}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$TAURI/icons/icon.icns"

cd "$TAURI"
cargo test --release
cargo tauri build

APP="$TAURI/target/release/bundle/macos/HotPipe.app"
[[ -d "$APP" ]] || { echo "собранный пакет не найден: $APP" >&2; exit 1; }
rm -rf "$OUT/HotPipe.app"
ditto "$APP" "$OUT/HotPipe.app"
echo "собрано: $OUT/HotPipe.app версии ${HOTPIPE_VERSION:-?}"

if [[ "$INSTALL" == "yes" ]]; then
	DST="$HOME/Applications/HotPipe.app"
	mkdir -p "$HOME/Applications"
	# Работающий сервер доживает на старом файле; новый пакет кладём целиком.
	rm -rf "$DST.new"
	ditto "$OUT/HotPipe.app" "$DST.new"
	rm -rf "$DST"
	mv "$DST.new" "$DST"
	echo "установлено: $DST"
	if curl -s --max-time 2 -o /dev/null "http://127.0.0.1:$PORT/api/status"; then
		# launchd поднимет завершившийся сервер — уже из нового пакета.
		echo "сервер работает — прошу перезапуститься на новом файле"
		curl -s --max-time 3 -X POST -H 'X-Hotpipe: 1' "http://127.0.0.1:$PORT/api/shutdown" >/dev/null || true
	fi
fi
