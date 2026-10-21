#!/bin/sh
set -eu

go run ./cmd/media-digest &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true' EXIT INT TERM
sleep 1
curl --fail-with-body --request POST \
  --header 'Content-Type: application/json' \
  --data @examples/media-backlog.json \
  http://localhost:8080/digests
