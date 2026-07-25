#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

mise run minio-setup

exec mise exec -- go run ./cmd/demo
