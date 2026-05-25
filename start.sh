#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"

DATABASE_PORT=15432
BACKEND_PORT=8000
FRONTEND_PORT=5173
PRISM_DATABASE_COMPOSE_PROJECT="${PRISM_DATABASE_COMPOSE_PROJECT:-prism}"
DATABASE_URL="postgres://prism:prism@localhost:${DATABASE_PORT}/prism?sslmode=disable"
ORIGINAL_PATH="$PATH"

load_dotenv_file() {
    local env_file="$1"
    local line

    if [[ ! -f "$env_file" ]]; then
        return 0
    fi

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

        [[ "$line" == *=* ]] || continue

        key="${line%%=*}"
        value="${line#*=}"

        key="${key%"${key##*[![:space:]]}"}"
        value="${value#"${value%%[![:space:]]*}"}"

        [[ -z "$key" ]] && continue
        [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue

        case "$key" in
            PATH|BASH_ENV|ENV|SHELLOPTS|BASHOPTS|CDPATH)
                continue
                ;;
        esac

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

MODE="${1:-full}"
case "$MODE" in
    full|headless) ;;
    *)
        echo "Usage: ./start.sh [full|headless]" >&2
        exit 1
        ;;
esac

load_dotenv_file "$ROOT_DIR/.env"
export PATH="$ORIGINAL_PATH"

resolve_config_path() {
    local path="${PRISM_CONFIG_PATH:-$ROOT_DIR/config.json}"

    case "$path" in
        /*) printf '%s\n' "$path" ;;
        ~/*) printf '%s/%s\n' "$HOME" "${path#~/}" ;;
        *) printf '%s/%s\n' "$ROOT_DIR" "$path" ;;
    esac
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "Error: missing required command: $1" >&2
        exit 1
    fi
}

postgres_compose() {
    (cd "$BACKEND_DIR" && docker compose --project-name "$PRISM_DATABASE_COMPOSE_PROJECT" "$@")
}

port_pids() {
    local port="$1"

    if command -v lsof >/dev/null 2>&1; then
        lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
        return
    fi

    if command -v fuser >/dev/null 2>&1; then
        local pid
        for pid in $(fuser -n tcp "$port" 2>/dev/null); do
            printf '%s\n' "$pid"
        done
        return
    fi

    echo "Error: need lsof or fuser to inspect port $port" >&2
    exit 1
}

pid_command() {
    ps -o command= -p "$1" 2>/dev/null || true
}

pid_exe() {
    readlink "/proc/$1/exe" 2>/dev/null || true
}

pid_cwd() {
    readlink "/proc/$1/cwd" 2>/dev/null || true
}

kill_pid() {
    local pid="$1"

    kill "$pid" 2>/dev/null || true
    sleep 1
    kill -0 "$pid" 2>/dev/null || return
    kill -9 "$pid" 2>/dev/null || true
}

is_prism_backend_pid() {
    local pid="$1"
    local exe
    local cwd
    local exe_name

    exe="$(pid_exe "$pid")"
    cwd="$(pid_cwd "$pid")"
    exe_name="${exe##*/}"

    [[ "$cwd" == "$BACKEND_DIR" ]] || return 1
    [[ "$exe" == "$BACKEND_BINARY" || "$exe" == "$BACKEND_BINARY (deleted)" || "$exe_name" == "prism-backend" || "$exe_name" == "prism-backend (deleted)" ]]
}

is_prism_frontend_pid() {
    local pid="$1"
    local cwd
    local command

    cwd="$(pid_cwd "$pid")"
    command="$(pid_command "$pid")"

    [[ "$cwd" == "$FRONTEND_DIR" ]] || return 1
    [[ "$command" == *vite* ]]
}

reclaim_backend_port() {
    local pid

    for pid in $(port_pids "$BACKEND_PORT"); do
        if is_prism_backend_pid "$pid"; then
            kill_pid "$pid"
            continue
        fi

        echo "Error: port $BACKEND_PORT is already in use by a non-Prism process (pid $pid)." >&2
        exit 1
    done
}

reclaim_frontend_port() {
    local pid

    for pid in $(port_pids "$FRONTEND_PORT"); do
        if is_prism_frontend_pid "$pid"; then
            kill_pid "$pid"
            continue
        fi

        echo "Error: port $FRONTEND_PORT is already in use by a non-Prism process (pid $pid)." >&2
        exit 1
    done
}

ensure_database_port_available() {
    local pid

    for pid in $(port_pids "$DATABASE_PORT"); do
        echo "Error: port $DATABASE_PORT is already in use by a non-Prism process (pid $pid)." >&2
        exit 1
    done
}

cleanup() {
    local exit_code=$?

    trap - EXIT INT TERM

    [[ -n "${BACKEND_PID:-}" ]] && kill "$BACKEND_PID" 2>/dev/null || true
    [[ -n "${FRONTEND_PID:-}" ]] && kill "$FRONTEND_PID" 2>/dev/null || true
    wait 2>/dev/null || true

    postgres_compose down --remove-orphans >/dev/null 2>&1 || true

    exit "$exit_code"
}
trap cleanup EXIT INT TERM

wait_for_postgres() {
    local attempts=60

    while (( attempts > 0 )); do
        if postgres_compose exec -T prism-postgres pg_isready -U prism -d prism >/dev/null 2>&1; then
            return
        fi

        sleep 1
        attempts=$((attempts - 1))
    done

    echo "Error: PostgreSQL did not become ready on localhost:${DATABASE_PORT}" >&2
    exit 1
}

ensure_bootstrap_config() {
    if [[ -f "$PRISM_CONFIG_PATH" ]]; then
        return
    fi

    (cd "$BACKEND_DIR" && PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1 "$BACKEND_BINARY" >/dev/null)
}

load_effective_startup_settings() {
    local line

    EFFECTIVE_CONFIG_PATH=""
    EFFECTIVE_SERVER_HOST=""
    EFFECTIVE_SERVER_PORT=""
    EFFECTIVE_DATABASE_URL=""

    while IFS= read -r line; do
        case "$line" in
            PRISM_CONFIG_PATH=*) EFFECTIVE_CONFIG_PATH="${line#PRISM_CONFIG_PATH=}" ;;
            SERVER_HOST=*) EFFECTIVE_SERVER_HOST="${line#SERVER_HOST=}" ;;
            SERVER_PORT=*) EFFECTIVE_SERVER_PORT="${line#SERVER_PORT=}" ;;
            DATABASE_URL=*) EFFECTIVE_DATABASE_URL="${line#DATABASE_URL=}" ;;
        esac
    done < <(cd "$BACKEND_DIR" && PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1 "$BACKEND_BINARY")
}

ensure_local_launcher_contract() {
    load_effective_startup_settings

    if [[ "$EFFECTIVE_CONFIG_PATH" != "$PRISM_CONFIG_PATH" ]]; then
        echo "Error: start.sh requires PRISM_CONFIG_PATH=$PRISM_CONFIG_PATH" >&2
        echo "Current bootstrap config resolves PRISM_CONFIG_PATH=$EFFECTIVE_CONFIG_PATH" >&2
        exit 1
    fi

    if [[ "$EFFECTIVE_SERVER_HOST" != "0.0.0.0" && "$EFFECTIVE_SERVER_HOST" != "127.0.0.1" && "$EFFECTIVE_SERVER_HOST" != "localhost" && "$EFFECTIVE_SERVER_HOST" != "::" && "$EFFECTIVE_SERVER_HOST" != "[::]" ]]; then
        echo "Error: start.sh requires a local backend host, got $EFFECTIVE_SERVER_HOST" >&2
        exit 1
    fi

    if [[ "$EFFECTIVE_SERVER_PORT" != "$BACKEND_PORT" ]]; then
        echo "Error: start.sh requires backend port $BACKEND_PORT, got $EFFECTIVE_SERVER_PORT" >&2
        exit 1
    fi

    if [[ "$EFFECTIVE_DATABASE_URL" != "$DATABASE_URL" ]]; then
        echo "Error: start.sh requires DATABASE_URL=$DATABASE_URL" >&2
        echo "Current bootstrap config resolves DATABASE_URL=$EFFECTIVE_DATABASE_URL" >&2
        exit 1
    fi
}

start_frontend() {
    [[ -d "$FRONTEND_DIR/node_modules" ]] || (cd "$FRONTEND_DIR" && pnpm install)

    (
        cd "$FRONTEND_DIR"
        env -u VITE_API_BASE \
            PRISM_VITE_PROXY_ENABLED=1 \
            PRISM_VITE_PROXY_TARGET="http://localhost:${BACKEND_PORT}" \
            pnpm exec vite --host 0.0.0.0 --port "$FRONTEND_PORT"
    ) &
    FRONTEND_PID=$!
}

require_cmd go
require_cmd docker
if [[ "$MODE" == "full" ]]; then
    require_cmd pnpm
fi
docker compose version >/dev/null

PRISM_CONFIG_PATH="$(resolve_config_path)"
export PRISM_CONFIG_PATH
export DATABASE_URL

BACKEND_BINARY="$BACKEND_DIR/prism-backend"

(cd "$BACKEND_DIR" && go build -o "$BACKEND_BINARY" ./cmd/prism-backend)
ensure_bootstrap_config
ensure_local_launcher_contract

postgres_compose down --remove-orphans >/dev/null 2>&1 || true
reclaim_backend_port
reclaim_frontend_port
ensure_database_port_available

postgres_compose up -d prism-postgres
wait_for_postgres

(cd "$BACKEND_DIR" && "$BACKEND_BINARY") &
BACKEND_PID=$!

if [[ "$MODE" == "full" ]]; then
    start_frontend
fi

echo "Backend:  http://localhost:${BACKEND_PORT}"
echo "Config:   $PRISM_CONFIG_PATH"
if [[ "$MODE" == "full" ]]; then
    echo "Frontend: http://localhost:${FRONTEND_PORT}"
fi

wait
