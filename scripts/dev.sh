#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-3000}"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="$DIR/web"
PID_FILE="/tmp/gpumesh-dev-server.pid"
TUNNEL_PID_FILE="/tmp/gpumesh-tunnel.pid"

cleanup() {
  echo ""
  echo "[gpumesh] shutting down..."

  if [ -f "$TUNNEL_PID_FILE" ]; then
    kill "$(cat "$TUNNEL_PID_FILE")" 2>/dev/null || true
    rm -f "$TUNNEL_PID_FILE"
  fi

  if [ -f "$PID_FILE" ]; then
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
    rm -f "$PID_FILE"
  fi

  echo "[gpumesh] done."
}
trap cleanup EXIT INT TERM

# Kill anything already on the port
if lsof -ti ":$PORT" >/dev/null 2>&1; then
  echo "[gpumesh] killing existing process on port $PORT..."
  kill "$(lsof -ti ":$PORT")" 2>/dev/null || true
  sleep 0.5
fi

# Start dev server
echo "[gpumesh] starting dev server on :$PORT from $WEB_DIR"
cd "$WEB_DIR"
python3 -m http.server "$PORT" --bind 0.0.0.0 &
echo $! > "$PID_FILE"
sleep 1

if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "[gpumesh] ERROR: server failed to start"
  exit 1
fi

# Start tunnel
echo "[gpumesh] starting localhost.run tunnel (80 -> localhost:$PORT)..."
ssh -o StrictHostKeyChecking=no -R "80:localhost:$PORT" localhost.run &
TUNNEL_PID=$!
echo $TUNNEL_PID > "$TUNNEL_PID_FILE"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Local:   http://localhost:$PORT/"
echo "  Network: http://$(hostname -I | awk '{print $1}'):$PORT/"
echo "  Tunnel:  check SSH output above for URL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Press Ctrl+C to stop"
echo ""

wait
