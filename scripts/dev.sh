#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-3000}"
DIR="$(cd "$(dirname "$0")/.." && pwd)"
WEB_DIR="$DIR/web"
PID_FILE="/tmp/gpumesh-dev-server.pid"
TUNNEL_PID_FILE="/tmp/gpumesh-tunnel.pid"
LOG_DIR="/tmp/gpumesh-logs"

usage() {
  echo "Usage: dev.sh [--daemon|-d] [--stop] [--status]"
  echo ""
  echo "  (no args)   Start server + tunnel in foreground (Ctrl+C to stop)"
  echo "  --daemon,-d  Start in background, survive session close"
  echo "  --stop       Stop background server + tunnel"
  echo "  --status     Show running status"
  exit 0
}

do_stop() {
  echo "[gpumesh] stopping..."
  if [ -f "$TUNNEL_PID_FILE" ]; then
    kill "$(cat "$TUNNEL_PID_FILE")" 2>/dev/null && echo "  tunnel stopped" || echo "  tunnel not running"
    rm -f "$TUNNEL_PID_FILE"
  fi
  if [ -f "$PID_FILE" ]; then
    kill "$(cat "$PID_FILE")" 2>/dev/null && echo "  server stopped" || echo "  server not running"
    rm -f "$PID_FILE"
  fi
  echo "[gpumesh] done."
}

do_status() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "server:  RUNNING pid=$(cat "$PID_FILE") port=$PORT"
  else
    echo "server:  stopped"
  fi
  if [ -f "$TUNNEL_PID_FILE" ] && kill -0 "$(cat "$TUNNEL_PID_FILE")" 2>/dev/null; then
    echo "tunnel:  RUNNING pid=$(cat "$TUNNEL_PID_FILE")"
    if [ -f "$LOG_DIR/tunnel.log" ]; then
      grep -oP 'https://\S+\.lhr\.life' "$LOG_DIR/tunnel.log" | tail -1 | xargs -I{} echo "url:     {}"
    fi
  else
    echo "tunnel:  stopped"
  fi
}

cleanup() {
  do_stop
}

# Parse args
MODE="foreground"
case "${1:-}" in
  --daemon|-d) MODE="daemon" ;;
  --stop)      do_stop; exit 0 ;;
  --status)    do_status; exit 0 ;;
  --help|-h)   usage ;;
  "")          ;;
  *)           echo "Unknown flag: $1"; usage ;;
esac

if [ "$MODE" != "daemon" ]; then
  trap cleanup EXIT INT TERM
fi

# Kill anything already on the port
if lsof -ti ":$PORT" >/dev/null 2>&1; then
  echo "[gpumesh] killing existing process on port $PORT..."
  kill "$(lsof -ti ":$PORT")" 2>/dev/null || true
  sleep 0.5
fi

# Start dev server
echo "[gpumesh] starting dev server on :$PORT from $WEB_DIR"
mkdir -p "$LOG_DIR"
cd "$WEB_DIR"

if [ "$MODE" = "daemon" ]; then
  nohup python3 -m http.server "$PORT" --bind 0.0.0.0 > "$LOG_DIR/server.log" 2>&1 &
else
  python3 -m http.server "$PORT" --bind 0.0.0.0 &
fi
echo $! > "$PID_FILE"
sleep 1

if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "[gpumesh] ERROR: server failed to start"
  exit 1
fi

# Start tunnel
echo "[gpumesh] starting localhost.run tunnel (80 -> localhost:$PORT)..."

if [ "$MODE" = "daemon" ]; then
  nohup ssh -o StrictHostKeyChecking=no -R "80:localhost:$PORT" localhost.run > "$LOG_DIR/tunnel.log" 2>&1 &
  TUNNEL_PID=$!
  echo $TUNNEL_PID > "$TUNNEL_PID_FILE"
  sleep 4

  TUNNEL_URL=$(grep -oP 'https://\S+\.lhr\.life' "$LOG_DIR/tunnel.log" 2>/dev/null | tail -1 || echo "(waiting...)")
  LAN_IP=$(hostname -I | awk '{print $1}')

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Server:  http://$LAN_IP:$PORT/"
  echo "  Tunnel:  $TUNNEL_URL"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  ./scripts/dev.sh --status   check status"
  echo "  ./scripts/dev.sh --stop     kill everything"
  echo "  Logs:    $LOG_DIR/"
  echo ""
else
  ssh -o StrictHostKeyChecking=no -R "80:localhost:$PORT" localhost.run &
  TUNNEL_PID=$!
  echo $TUNNEL_PID > "$TUNNEL_PID_FILE"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Local:   http://localhost:$PORT/"
  echo "  Network: http://$(hostname -I | awk '{print $1}'):$PORT/"
  echo "  Tunnel:  check SSH output above for URL"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Press Ctrl+C to stop"
  echo ""

  wait
fi
