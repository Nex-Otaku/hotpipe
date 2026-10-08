#!/usr/bin/env bash
# Сборка hotpipe-server (из WSL).
#
# Результат кладётся в build/ и в хранилище не попадает. Порядок нумерации тот
# же, что в wsldrive: 0.1.<число снимков>-alpha (docs/decisions.md, Р-009).
#
#   build/build.sh            собрать и прогнать проверки
#   build/build.sh -race      собрать с обнаружением гонок
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/build"
PKG="github.com/Nex-Otaku/hotpipe/internal/version"

if git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1 && git -C "$ROOT" rev-parse HEAD >/dev/null 2>&1; then
	BUILD_NUM="$(git -C "$ROOT" rev-list --count HEAD)"
	COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
	if git -C "$ROOT" diff --quiet HEAD 2>/dev/null; then DIRTY=no; else DIRTY=yes; fi
else
	BUILD_NUM=0
	COMMIT=вне-хранилища
	DIRTY=yes
fi

VERSION="0.1.${BUILD_NUM}-alpha"
BUILT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X ${PKG}.Number=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Built=${BUILT} -X ${PKG}.Dirty=${DIRTY}"

mkdir -p "$OUT"
cd "$ROOT/src"

echo "версия ${VERSION}, снимок ${COMMIT}, изменённое дерево: ${DIRTY}"
go vet ./...
# Живая проверка (настоящий ssh по .env) здесь отключена: сборка не должна
# зависеть от сети. Пустое значение в окружении важнее .env.
HOTPIPE_LIVE_HOST= go test -count=1 ./... >/dev/null
# -trimpath убирает пути сборочной машины из двоичного файла.
go build -trimpath -ldflags "$LDFLAGS" "$@" -o "$OUT/hotpipe-server" ./cmd/hotpipe-server
echo "собрано: $OUT/hotpipe-server"
"$OUT/hotpipe-server" -version
