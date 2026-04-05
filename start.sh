#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

load_dotenv_file() {
    local env_file="$1"
    local line

    if [[ ! -f "$env_file" ]]; then
        return
    fi

    echo "Loading environment from $env_file"

    while IFS= read -r line || [[ -n "$line" ]]; do
        local key
        local value

        line="${line%$'\r'}"
        line="${line#"${line%%[![:space:]]*}"}"

        [[ -z "$line" || "${line:0:1}" == "#" ]] && continue

        if [[ "$line" == export[[:space:]]* ]]; then
            line="${line#export}"
            line="${line#"${line%%[![:space:]]*}"}"
        fi

        [[ "$line" != *=* ]] && continue

        key="${line%%=*}"
        value="${line#*=}"

        key="${key%"${key##*[![:space:]]}"}"
        value="${value#"${value%%[![:space:]]*}"}"

        [[ -z "$key" ]] && continue
        if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
            echo "Warning: skipping invalid environment key in $env_file: $key"
            continue
        fi

        if [[ -n "${!key+x}" ]]; then
            continue
        fi

        if [[ "$value" =~ ^\"(.*)\"$ ]]; then
            value="${BASH_REMATCH[1]}"
            value="${value//\\n/$'\n'}"
            value="${value//\\r/$'\r'}"
            value="${value//\\t/$'\t'}"
            value="${value//\\\"/\"}"
            value="${value//\\\\/\\}"
        elif [[ "$value" =~ ^\'(.*)\'$ ]]; then
            value="${BASH_REMATCH[1]}"
        else
            value="${value%%[[:space:]]#*}"
            value="${value%"${value##*[![:space:]]}"}"
        fi

        export "$key=$value"
    done < "$env_file"
}

load_dotenv_file "$ROOT_DIR/.env"

BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_UV_BIN="${BACKEND_UV_BIN:-uv}"
BACKEND_PYTHON_BIN="${BACKEND_PYTHON_BIN:-python3.13}"
DATABASE_PORT=15432
BACKEND_PORT=18000
FRONTEND_PORT=15173
MODE="${1:-full}"
FRONTEND_ORIGIN="http://localhost:${FRONTEND_PORT}"
FRONTEND_LOOPBACK_ORIGIN="http://127.0.0.1:${FRONTEND_PORT}"
DEFAULT_DATABASE_URL="postgresql+asyncpg://prism:prism@localhost:${DATABASE_PORT}/prism"
DATABASE_URL_FROM_ENV=true
if [[ -z "${DATABASE_URL:-}" ]]; then
    DATABASE_URL_FROM_ENV=false
fi
DATABASE_URL="${DATABASE_URL:-$DEFAULT_DATABASE_URL}"
export DATABASE_URL
CLEANED_UP=false

append_csv_value_if_missing() {
    local existing_values="$1"
    local value_to_add="$2"
    local part
    local -a existing_parts

    if [[ -n "$existing_values" ]]; then
        IFS=',' read -r -a existing_parts <<< "$existing_values"
        for part in "${existing_parts[@]}"; do
            part="${part#"${part%%[![:space:]]*}"}"
            part="${part%"${part##*[![:space:]]}"}"
            if [[ "$part" == "$value_to_add" ]]; then
                printf '%s' "$existing_values"
                return
            fi
        done

        printf '%s,%s' "$existing_values" "$value_to_add"
        return
    fi

    printf '%s' "$value_to_add"
}

configure_local_frontend_backend_integration() {
    local cors_origins="${CORS_ALLOWED_ORIGINS:-}"

    cors_origins="$(append_csv_value_if_missing "$cors_origins" "$FRONTEND_ORIGIN")"
    cors_origins="$(append_csv_value_if_missing "$cors_origins" "$FRONTEND_LOOPBACK_ORIGIN")"

    export CORS_ALLOWED_ORIGINS="$cors_origins"
    export WEBAUTHN_ORIGIN="${WEBAUTHN_ORIGIN:-$FRONTEND_ORIGIN}"
}

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

backend_uv() {
    (cd "$BACKEND_DIR" && "$BACKEND_UV_BIN" "$@")
}

parse_database_host_port() {
    local database_url="$1"

    backend_uv run --no-sync --python "$BACKEND_PYTHON_BIN" -- python - "$database_url" <<'PY'
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

    backend_uv run --no-sync --python "$BACKEND_PYTHON_BIN" -- python - "$host" "$port" <<'PY'
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

ensure_docker_compose_available() {
    if ! command -v docker >/dev/null 2>&1; then
        echo "Error: docker is required to start the local PostgreSQL service."
        exit 1
    fi

    if ! docker compose version >/dev/null 2>&1; then
        echo "Error: docker compose is required to start the local PostgreSQL service."
        exit 1
    fi
}

stop_database_container() {
    if ! command -v docker >/dev/null 2>&1; then
        return
    fi

    if ! docker compose version >/dev/null 2>&1; then
        return
    fi

    (cd "$BACKEND_DIR" && docker compose down --remove-orphans >/dev/null 2>&1) || true
}

start_database_container() {
    ensure_docker_compose_available

    echo "Starting PostgreSQL via docker compose on port $DATABASE_PORT..."
    (cd "$BACKEND_DIR" && docker compose up -d postgres)
}

wait_for_database_container() {
    local attempts=60

    while [ "$attempts" -gt 0 ]; do
        if (cd "$BACKEND_DIR" && docker compose exec -T postgres pg_isready -U prism -d prism >/dev/null 2>&1); then
            return
        fi

        sleep 1
        attempts=$((attempts - 1))
    done

    echo "Error: PostgreSQL did not become ready in time."
    exit 1
}

ensure_backend_database_ready() {
    local database_url
    local host_port
    local db_host
    local db_port
    local attempts=30

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

    while [ "$attempts" -gt 0 ]; do
        if tcp_port_open "$db_host" "$db_port"; then
            return
        fi

        sleep 1
        attempts=$((attempts - 1))
    done

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
    stop_database_container
    kill_running_on_port "$DATABASE_PORT" "database"
    kill_running_on_port "$BACKEND_PORT" "backend"
    kill_running_on_port "$FRONTEND_PORT" "frontend"
}

cleanup() {
    if [ "$CLEANED_UP" = true ]; then
        return
    fi
    CLEANED_UP=true
    trap - EXIT INT TERM
    trap '' INT TERM

    echo ""
    echo "Shutting down..."
    [[ -n "${BACKEND_PID:-}" ]] && kill "$BACKEND_PID" 2>/dev/null
    [[ -n "${FRONTEND_PID:-}" ]] && kill "$FRONTEND_PID" 2>/dev/null
    wait 2>/dev/null || true
    stop_database_container
    kill_running_on_port "$BACKEND_PORT" "backend"
    kill_running_on_port "$FRONTEND_PORT" "frontend"
    kill_running_on_port "$DATABASE_PORT" "database"
    echo "Done."
}
trap cleanup EXIT INT TERM

configure_local_frontend_backend_integration

kill_existing_instances

# --- Backend setup ---
if ! command -v "$BACKEND_UV_BIN" >/dev/null 2>&1; then
    echo "Error: $BACKEND_UV_BIN is required to manage the backend environment."
    exit 1
fi

if ! command -v "$BACKEND_PYTHON_BIN" >/dev/null 2>&1; then
    echo "Error: $BACKEND_PYTHON_BIN is required to create the backend uv environment."
    echo "Set BACKEND_PYTHON_BIN to a Python 3.13 interpreter if needed."
    exit 1
fi

start_database_container

echo "Syncing backend dependencies with uv..."
backend_uv sync --locked --python "$BACKEND_PYTHON_BIN"
wait_for_database_container
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
(cd "$BACKEND_DIR" && "$BACKEND_UV_BIN" run --no-sync --python "$BACKEND_PYTHON_BIN" prism-backend --reload --port "$BACKEND_PORT") &
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
