#!/usr/bin/env sh
# Requires Go, Node/npm, Python 3.14+ and native Wails toolchain dependencies.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if ! command -v python3 >/dev/null 2>&1; then
    echo 'Python 3.14+ is required for building. See build/README.md.' >&2
    exit 1
fi
exec python3 -I "$ROOT/scripts/build.py" "$@"
