#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_PORT="${BACKEND_PORT:-8000}"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
MODE="${1:-full}"
DEFAULT_DATABASE_URL="postgresql+asyncpg://prism:prism@localhost:5432/prism"
DATABASE_URL_FROM_ENV=true
if [[ -z "${DATABASE_URL:-}" ]]; then
    DATABASE_URL_FROM_ENV=false
fi
DATABASE_URL="${DATABASE_URL:-$DEFAULT_DATABASE_URL}"
export DATABASE_URL
CLEANED_UP=false

usage() {
    echo "Usage: $0 [headless|full]"
    echo ""
    echo "Modes:"
    echo "  headless  Start backend only"
    echo "  full      Start backend + frontend (default)"
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

read_backend_database_url() {
    echo "$DATABASE_URL"
}

parse_database_host_port() {
    local database_url="$1"

    "$BACKEND_DIR/venv/bin/python" - "$database_url" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
host = parsed.hostname
port = parsed.port

if not host or port is None:
    sys.exit(1)

print(f"{host} {port}")
PY
}

tcp_port_open() {
    local host="$1"
    local port="$2"

    "$BACKEND_DIR/venv/bin/python" - "$host" "$port" <<'PY'
import socket
import sys

host = sys.argv[1]
port = int(sys.argv[2])

try:
    with socket.create_connection((host, port), timeout=1.5):
        pass
except OSError:
    sys.exit(1)

sys.exit(0)
PY
}

ensure_backend_database_ready() {
    local database_url
    local host_port
    local db_host
    local db_port

    database_url="$(read_backend_database_url)"
    if [ "$DATABASE_URL_FROM_ENV" = false ]; then
        echo "DATABASE_URL is not set; using default: $database_url"
    fi
    echo "Backend database URL: $database_url"

    case "$database_url" in
        [Pp][Oo][Ss][Tt][Gg][Rr][Ee][Ss][Qq][Ll]*)
            ;;
        *)
            echo "Error: DATABASE_URL must point to PostgreSQL."
            echo "Current value: $database_url"
            exit 1
            ;;
    esac

    if ! host_port="$(parse_database_host_port "$database_url")"; then
        echo "Error: DATABASE_URL must include both host and port."
        echo "Current value: $database_url"
        exit 1
    fi

    if ! read -r db_host db_port <<<"$host_port"; then
        echo "Error: DATABASE_URL must include both host and port."
        echo "Current value: $database_url"
        exit 1
    fi
    if [[ -z "$db_host" || -z "$db_port" ]]; then
        echo "Error: DATABASE_URL must include both host and port."
        echo "Current value: $database_url"
        exit 1
    fi

    if tcp_port_open "$db_host" "$db_port"; then
        return
    fi

    echo "PostgreSQL is not reachable at $db_host:$db_port."
    echo "Start your database first, then retry (example: cd backend && docker compose up -d postgres)."
    exit 1
}

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
ensure_backend_database_ready

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
