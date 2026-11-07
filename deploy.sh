#!/usr/bin/env bash
# Deploy jusdoit to the Oracle server.
# Usage: ./deploy.sh
set -euo pipefail

HOST="ubuntu@140.245.245.50"
KEY="$HOME/Downloads/keys/ssh-key-2025-12-01.key"
DEST="/var/www/jusdoit/index.html"
REMOTE="http://140.245.245.50/"

cd "$(dirname "$0")"

echo "==> Uploading index.html"
scp -q -i "$KEY" -o StrictHostKeyChecking=accept-new index.html "$HOST:$DEST"

echo "==> Verifying"
LOCAL_MD5=$(md5sum index.html | cut -d' ' -f1)
REMOTE_MD5=$(ssh -i "$KEY" "$HOST" "md5sum $DEST" | cut -d' ' -f1)

if [ "$LOCAL_MD5" != "$REMOTE_MD5" ]; then
  echo "!! Checksum mismatch: local=$LOCAL_MD5 remote=$REMOTE_MD5"
  exit 1
fi

echo "==> Checksum OK: $LOCAL_MD5"
curl -fsS -o /dev/null -w "==> Live: HTTP %{http_code} (%{size_download} bytes)\n" "$REMOTE"
