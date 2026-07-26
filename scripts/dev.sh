#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

# Bucket setup (also waits for MinIO health). Keep in dev — overmind stops all
# processes when a one-shot Procfile entry exits.
mise run minio-setup

# Orphan ./tmp/dev from a prior air/overmind session holds DEV_HTTP_ADDR.
"$(dirname "$0")/free-dev-port.sh"

exec mise exec -- air -c .air.toml
