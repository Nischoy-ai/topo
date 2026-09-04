#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

exec env GOTOOLCHAIN=go1.26.8 go run ./internal/productionchecktool "$@"
