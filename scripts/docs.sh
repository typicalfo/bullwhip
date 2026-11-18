#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
go run ./cmd/bw help --markdown > README.md
