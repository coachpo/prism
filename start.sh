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

read_backend_database_url() {
    (cd "$BACKEND_DIR" && DATABASE_URL="${DATABASE_URL:-}" ./venv/bin/python - <<'PY'
import os
from pathlib import Path

database_url = os.environ.get("DATABASE_URL", "").strip()

if not database_url:
    env_path = Path(".env")
    if env_path.exists():
        for line in env_path.read_text().splitlines():
            stripped = line.strip()
            if not stripped or stripped.startswith("#") or "=" not in stripped:
                continue

            key, value = stripped.split("=", 1)
            if key.strip() == "DATABASE_URL":
                database_url = value.strip().strip('"').strip("'")
                break

if not database_url:
    database_url = "postgresql+asyncpg://prism:prism@localhost:5432/prism"

print(database_url)
PY
    )
}

parse_database_host_port() {
    local database_url="$1"

    "$BACKEND_DIR/venv/bin/python" - "$database_url" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
host = parsed.hostname or "localhost"
port = parsed.port or 5432
is_local = "true" if host in {"localhost", "127.0.0.1", "::1"} else "false"

print(f"{host} {port} {is_local}")
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

wait_for_tcp_port() {
    local host="$1"
    local port="$2"
    local timeout_seconds="${3:-45}"
    local elapsed=0

    while [ "$elapsed" -lt "$timeout_seconds" ]; do
        if tcp_port_open "$host" "$port"; then
            return 0
        fi

        sleep 1
        elapsed=$((elapsed + 1))
    done

    return 1
}

ensure_backend_database_ready() {
    local database_url
    local db_host
    local db_port
    local db_is_local

    database_url="$(read_backend_database_url)"
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

    read -r db_host db_port db_is_local <<<"$(parse_database_host_port "$database_url")"

    if tcp_port_open "$db_host" "$db_port"; then
        return
    fi

    if [ "$db_is_local" = "true" ] && [ -f "$BACKEND_DIR/docker-compose.yml" ] && command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        echo "PostgreSQL is not reachable at $db_host:$db_port. Starting local postgres via Docker Compose..."
        if ! (cd "$BACKEND_DIR" && docker compose up -d postgres); then
            echo "Failed to start postgres with Docker Compose."
            exit 1
        fi

        if wait_for_tcp_port "$db_host" "$db_port" 45; then
            echo "PostgreSQL is ready at $db_host:$db_port."
            return
        fi

        echo "PostgreSQL did not become reachable at $db_host:$db_port within timeout."
        exit 1
    fi

    echo "PostgreSQL is not reachable at $db_host:$db_port."
    echo "Start your database first or set DATABASE_URL to a reachable PostgreSQL instance."
    if [ -f "$BACKEND_DIR/docker-compose.yml" ]; then
        echo "Hint: cd backend && docker compose up -d postgres"
    fi
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
