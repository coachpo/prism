#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_PORT="${BACKEND_PORT:-8000}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
MODE="${1:-${START_MODE:-headless}}"
CLEANED_UP=false

usage() {
    echo "Usage: $0 [headless|full]"
    echo ""
    echo "Modes:"
    echo "  headless  Start backend only (default)"
    echo "  full      Start backend + frontend"
    echo ""
    echo "You can also set START_MODE=headless|full."
}

if [[ "${MODE}" == "-h" || "${MODE}" == "--help" ]]; then
    usage
    exit 0
fi

if [[ "$#" -gt 1 ]]; then
    usage
    exit 1
fi

case "$MODE" in
    headless)
        START_FRONTEND=false
        ;;
    full)
        START_FRONTEND=true
        ;;
    *)
        echo "Invalid mode: $MODE"
        usage
        exit 1
        ;;
esac

port_listeners() {
    local port="$1"

    if ! command -v lsof >/dev/null 2>&1; then
        return 0
    fi

    lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
}

kill_running_on_port() {
    local port="$1"
    local name="$2"
    local pids

    pids="$(port_listeners "$port")"
    if [ -z "$pids" ]; then
        return
    fi

    echo "Stopping existing $name process(es) on port $port..."
    kill $pids 2>/dev/null || true

    local attempts=20
    while [ "$attempts" -gt 0 ] && [ -n "$(port_listeners "$port")" ]; do
        sleep 0.25
        attempts=$((attempts - 1))
    done

    pids="$(port_listeners "$port")"
    if [ -n "$pids" ]; then
        echo "Force-stopping stubborn process(es) on port $port..."
        kill -9 $pids 2>/dev/null || true
    fi
}

kill_existing_instances() {
    kill_running_on_port "$BACKEND_PORT" "backend"

    if [ "$START_FRONTEND" = true ]; then
        kill_running_on_port "$FRONTEND_PORT" "frontend"
    fi
}

cleanup() {
    if [ "$CLEANED_UP" = true ]; then
        return
    fi
    CLEANED_UP=true

    echo ""
    echo "Shutting down..."
    [[ -n "${BACKEND_PID:-}" ]] && kill "$BACKEND_PID" 2>/dev/null
    [[ -n "${FRONTEND_PID:-}" ]] && kill "$FRONTEND_PID" 2>/dev/null
    wait 2>/dev/null
    echo "Done."
}
trap cleanup EXIT INT TERM

kill_existing_instances

# --- Backend setup ---
if [ ! -d "$BACKEND_DIR/venv" ]; then
    echo "Creating Python virtual environment..."
    python3 -m venv "$BACKEND_DIR/venv"
fi

echo "Installing backend dependencies..."
"$BACKEND_DIR/venv/bin/pip" install -q -r "$BACKEND_DIR/requirements.txt"

if [ "$START_FRONTEND" = true ]; then
    # --- Frontend setup ---
    if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
        echo "Installing frontend dependencies..."
        (cd "$FRONTEND_DIR" && pnpm install)
    fi
fi

# --- Start backend ---
echo "Starting backend on port $BACKEND_PORT..."
(cd "$BACKEND_DIR" && ./venv/bin/python -m uvicorn app.main:app \
    --host 0.0.0.0 --port "$BACKEND_PORT" --reload) &
BACKEND_PID=$!

if [ "$START_FRONTEND" = true ]; then
    # --- Start frontend ---
    # Frontend calls backend directly (no dev proxy) via VITE_API_BASE
    echo "Starting frontend on port $FRONTEND_PORT..."
    (cd "$FRONTEND_DIR" && VITE_API_BASE="http://localhost:$BACKEND_PORT" \
        pnpm exec vite --port "$FRONTEND_PORT" --host) &
    FRONTEND_PID=$!
fi

echo ""
echo "========================================="
echo "  LLM Proxy Gateway"
echo "  Mode:     $MODE"
echo "  Backend:  http://localhost:$BACKEND_PORT"
if [ "$START_FRONTEND" = true ]; then
    echo "  Frontend: http://localhost:$FRONTEND_PORT"
else
    echo "  Frontend: disabled (headless mode)"
fi
echo "  API Docs: http://localhost:$BACKEND_PORT/docs"
echo "========================================="
echo ""

wait
