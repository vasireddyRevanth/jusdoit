#!/usr/bin/env bash
# Build and deploy jusdoit (API + static site) to the Oracle server.
# Usage: ./deploy.sh
set -euo pipefail

HOST="${HOST:-ubuntu@140.245.245.50}"
KEY="${KEY:-$HOME/Downloads/keys/ssh-key-2025-12-01.key}"
BASE="${BASE:-http://140.245.245.50}"

cd "$(dirname "$0")"

VERSION="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
mkdir -p dist

echo "==> Building ($VERSION)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o dist/jusdoit ./cmd/server

echo "==> Uploading binary"
scp -q -i "$KEY" -o StrictHostKeyChecking=accept-new dist/jusdoit "$HOST:/tmp/jusdoit.new"

echo "==> Uploading static site"
scp -q -i "$KEY" index.html "$HOST:/var/www/jusdoit/index.html"

echo "==> Installing and restarting"
ssh -i "$KEY" "$HOST" \
  "sudo install -m 0755 /tmp/jusdoit.new /usr/local/bin/jusdoit && rm -f /tmp/jusdoit.new && sudo systemctl restart jusdoit"

echo "==> Verifying"
for _ in $(seq 1 30); do
  if curl -fsS "$BASE/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -fsS "$BASE/api/health"; echo
curl -fsS -o /dev/null -w "==> site HTTP %{http_code}\n" "$BASE/"
