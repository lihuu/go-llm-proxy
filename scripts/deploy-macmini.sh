#!/bin/bash
# Minimal-downtime deploy of go-llm-proxy to the Mac mini.
#
# Order of operations (the proxy only goes down for the last ~3s):
#   1. Build the darwin/arm64 binary locally (proxy stays up on the mini).
#   2. scp it to the mini as go-llm-proxy.new (proxy still up).
#   3. Over the wire: swap binary in-place + restart via launchctl.
#
# Qwen Code / any coding agent should invoke this single script instead of
# stopping the service before building/copying. This keeps the proxy
# reachable throughout the build/copy phase; downtime is limited to the
# bootout -> bootstrap gap (~3s).

set -euo pipefail

REMOTE_HOST="${REMOTE_HOST:-macmini}"
REMOTE_USER=$(/usr/bin/ssh "$REMOTE_HOST" 'id -un')
REMOTE_DIR="/Users/$REMOTE_USER/deploy/llm-proxy"
BINARY="go-llm-proxy"
TMP_REMOTE_BIN="$REMOTE_DIR/$BINARY.new"

echo "==> Building darwin/arm64 binary locally..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w" -o "$BINARY" .

echo "==> Copying to $REMOTE_HOST:$TMP_REMOTE_BIN..."
/usr/bin/scp -q "$BINARY" "$REMOTE_HOST:$TMP_REMOTE_BIN"

echo "==> Swapping binary and restarting on $REMOTE_HOST..."
# Do the swap + restart atomically from the remote side so the proxy is down
# only for the bootout -> bootstrap window (~3s), not during build/copy.
/usr/bin/ssh "$REMOTE_HOST" bash -s -- "$REMOTE_DIR" "$BINARY" <<'REMOTE_SCRIPT'
set -euo pipefail
REMOTE_DIR="$1"
BINARY="$2"
PLIST="$HOME/Library/LaunchAgents/com.llm-proxy.plist"
LABEL="com.llm-proxy"
DOMAIN="gui/$(id -u)/$LABEL"

cd "$REMOTE_DIR"

# Backup current binary, then move the new one into place.
if [ -f "$BINARY" ]; then
  cp -p "$BINARY" "$BINARY.bak"
fi
mv -f "$BINARY.new" "$BINARY"
chmod +x "$BINARY"

echo "Stopping $LABEL..."
launchctl bootout "$DOMAIN" 2>/dev/null || true

# Wait until launchd has fully unloaded the service. bootout is asynchronous:
# if we bootstrap before unregistration completes, we get "Input/output error".
for i in $(seq 1 10); do
  sleep 1
  if ! launchctl print "$DOMAIN" >/dev/null 2>&1; then
    break
  fi
done

echo "Starting $LABEL..."
launchctl bootstrap "gui/$(id -u)" "$PLIST"

# Poll for up to 15s — launchd registration can lag behind the bootstrap call,
# especially with ThrottleInterval set.
PID=""
for i in $(seq 1 15); do
  sleep 1
  if PID=$(pgrep -f "$BINARY" 2>/dev/null | head -1); [ -n "$PID" ]; then
    break
  fi
done

if [ -n "$PID" ]; then
  echo "OK: service is running (PID $PID)"
else
  echo "ERROR: service failed to start" >&2
  echo "--- stdout tail ---" >&2
  tail -5 "$REMOTE_DIR/stdout.log" >&2 2>/dev/null || true
  echo "--- stderr tail ---" >&2
  tail -5 "$REMOTE_DIR/stderr.log" >&2 2>/dev/null || true
  exit 1
fi
REMOTE_SCRIPT

echo "==> Done. Proxy downtime was limited to the restart window (~3s)."