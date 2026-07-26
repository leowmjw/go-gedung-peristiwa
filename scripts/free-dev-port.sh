#!/usr/bin/env bash
# Reclaim DEV_HTTP_ADDR when a stale ./tmp/dev listener is left behind (air/overmind restart).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
addr="${DEV_HTTP_ADDR:-:8080}"
port="${addr##*:}"

if ! command -v lsof >/dev/null 2>&1; then
	exit 0
fi

pids=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -z "$pids" ]; then
	exit 0
fi

is_our_dev() {
	local pid=$1
	local exe
	exe=$(lsof -p "$pid" 2>/dev/null | awk '$4=="txt" && $NF ~ /\/tmp\/dev$/ {print $NF; exit}')
	[ -n "$exe" ] && [ "$exe" = "$root/tmp/dev" ]
}

reclaimed=0
for pid in $pids; do
	if is_our_dev "$pid"; then
		echo "reclaiming stale dev listener on :$port (pid $pid)"
		kill -INT "$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
		reclaimed=1
		continue
	fi
	cmd=$(ps -p "$pid" -o command= 2>/dev/null || echo "unknown")
	echo "error: :$port already in use by pid $pid ($cmd)" >&2
	echo "→ stop that process, or set DEV_HTTP_ADDR to another port in .env" >&2
	exit 1
done

if [ "$reclaimed" -eq 1 ]; then
	for _ in $(seq 1 20); do
		if ! lsof -nP -iTCP:"$port" -sTCP:LISTEN -t >/dev/null 2>&1; then
			exit 0
		fi
		sleep 0.1
	done
	echo "error: :$port still in use after stopping stale dev" >&2
	exit 1
fi
