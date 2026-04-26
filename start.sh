#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"

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
load_dotenv_file "$BACKEND_DIR/.env"
load_dotenv_file "$FRONTEND_DIR/.env"

BACKEND_GO_BIN="${BACKEND_GO_BIN:-go}"
FRONTEND_PNPM_BIN="${FRONTEND_PNPM_BIN:-pnpm}"
DATABASE_PORT=15432
BACKEND_PORT=18000
FRONTEND_PORT=15173
MODE="${1:-full}"
FRONTEND_ORIGIN="http://localhost:${FRONTEND_PORT}"
FRONTEND_LOOPBACK_ORIGIN="http://127.0.0.1:${FRONTEND_PORT}"
DEFAULT_DATABASE_URL="postgres://prism:prism@localhost:${DATABASE_PORT}/prism?sslmode=disable"
DATABASE_URL_FROM_ENV=true
if [[ -z "${DATABASE_URL:-}" ]]; then
    DATABASE_URL_FROM_ENV=false
fi
DATABASE_URL="${DATABASE_URL:-$DEFAULT_DATABASE_URL}"
export DATABASE_URL
LAUNCHER_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/prism-start.XXXXXX")"
BACKEND_BINARY_PATH="$LAUNCHER_TMP_DIR/prism-backend"
BOOTSTRAP_CONFIG_PATH_FROM_ENV=true
if [[ -z "${PRISM_CONFIG_PATH:-}" ]]; then
    BOOTSTRAP_CONFIG_PATH_FROM_ENV=false
fi
EFFECTIVE_BACKEND_HOST=""
EFFECTIVE_BACKEND_PORT=""
EFFECTIVE_BACKEND_ADDR=""
EFFECTIVE_DATABASE_URL=""
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

resolve_launcher_bootstrap_config_path() {
    local configured_path="${PRISM_CONFIG_PATH:-}"

    if [[ -z "$configured_path" ]]; then
        printf '%s' "$LAUNCHER_TMP_DIR/bootstrap-config.json"
        return
    fi

    case "$configured_path" in
        /*)
            printf '%s' "$configured_path"
            ;;
        ~/*)
            printf '%s/%s' "$HOME" "${configured_path#~/}"
            ;;
        *)
            printf '%s/%s' "$ROOT_DIR" "$configured_path"
            ;;
    esac
}

configure_bootstrap_startup_contract() {
    PRISM_CONFIG_PATH="$(resolve_launcher_bootstrap_config_path)"
    export PRISM_CONFIG_PATH
}

configure_launcher_bootstrap_seed_inputs() {
    export PORT="$BACKEND_PORT"
}

BOOTSTRAP_SEED_ENV_VARS=(
    DATABASE_URL
    HOST
    PORT
    APP_ENV
    RUNTIME_TELEMETRY_MODE
    RUNTIME_BUFFERING_MODE
    RUNTIME_TRANSPORT_MAX_IDLE_CONNS
    RUNTIME_TRANSPORT_MAX_IDLE_CONNS_PER_HOST
    RUNTIME_TRANSPORT_MAX_CONNS_PER_HOST
    RUNTIME_TRANSPORT_IDLE_CONN_TIMEOUT
    RUNTIME_TRANSPORT_RESPONSE_HEADER_TIMEOUT
    RUNTIME_TRANSPORT_TLS_HANDSHAKE_TIMEOUT
    RUNTIME_TRANSPORT_EXPECT_CONTINUE_TIMEOUT
    RUNTIME_DB_MAX_CONNS
    RUNTIME_DB_MIN_IDLE_CONNS
    MANAGEMENT_DB_MAX_CONNS
    MANAGEMENT_DB_MIN_IDLE_CONNS
    MANAGEMENT_ADMISSION_M2_MAX_CONCURRENT
    MANAGEMENT_ADMISSION_M3_MAX_CONCURRENT
    SECRET_ENCRYPTION_KEY
    CONFIG_BUNDLE_ENCRYPTION_KEY
    CORS_ALLOWED_ORIGINS
    AUTH_JWT_SECRET
    AUTH_ACCESS_TOKEN_TTL_SECONDS
    AUTH_REFRESH_TOKEN_TTL_SECONDS
    AUTH_RESET_CODE_TTL_SECONDS
    AUTH_COOKIE_NAME
    AUTH_REFRESH_COOKIE_NAME
    AUTH_COOKIE_SECURE
)

run_backend_with_bootstrap_config() {
    local -a env_args=(env)
    local env_name

    for env_name in "${BOOTSTRAP_SEED_ENV_VARS[@]}"; do
        env_args+=(-u "$env_name")
    done

    (cd "$BACKEND_DIR" && "${env_args[@]}" "$@")
}

ensure_bootstrap_config_exists() {
    if [[ -f "$PRISM_CONFIG_PATH" ]]; then
        return
    fi

    echo "Seeding plaintext bootstrap config: $PRISM_CONFIG_PATH"
    if ! (cd "$BACKEND_DIR" && PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1 "$BACKEND_BINARY_PATH" >/dev/null); then
        echo "Error: failed to seed bootstrap config at $PRISM_CONFIG_PATH."
        echo "Check PRISM_CONFIG_PATH and any one-time seed inputs required to create the plaintext bootstrap file."
        exit 1
    fi
}

resolve_effective_backend_startup_settings() {
    local startup_settings_output
    local line
    local key
    local value

    EFFECTIVE_DATABASE_URL=""
    EFFECTIVE_BACKEND_HOST=""
    EFFECTIVE_BACKEND_PORT=""
    EFFECTIVE_BACKEND_ADDR=""

    if ! startup_settings_output="$(run_backend_with_bootstrap_config PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS=1 "$BACKEND_BINARY_PATH")"; then
        echo "Error: failed to resolve effective backend startup settings from the bootstrap startup contract."
        echo "Check PRISM_CONFIG_PATH and the plaintext bootstrap file content."
        exit 1
    fi

    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" || "$line" != *=* ]] && continue
        key="${line%%=*}"
        value="${line#*=}"
        case "$key" in
            DATABASE_URL)
                EFFECTIVE_DATABASE_URL="$value"
                ;;
            HOST)
                EFFECTIVE_BACKEND_HOST="$value"
                ;;
            PORT)
                EFFECTIVE_BACKEND_PORT="$value"
                ;;
            ADDR)
                EFFECTIVE_BACKEND_ADDR="$value"
                ;;
        esac
    done <<< "$startup_settings_output"

    if [[ -z "$EFFECTIVE_DATABASE_URL" || -z "$EFFECTIVE_BACKEND_HOST" || -z "$EFFECTIVE_BACKEND_PORT" || -z "$EFFECTIVE_BACKEND_ADDR" ]]; then
        echo "Error: backend startup settings helper returned incomplete output."
        exit 1
    fi
}

is_launcher_compatible_backend_host() {
    local host="$1"
    case "$host" in
        0.0.0.0|127.0.0.1|localhost|::|[::])
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

ensure_launcher_backend_binding_matches_expectations() {
    if [[ "$EFFECTIVE_BACKEND_PORT" != "$BACKEND_PORT" ]]; then
        echo "Error: launcher mode expects backend port $BACKEND_PORT but bootstrap config resolves port $EFFECTIVE_BACKEND_PORT."
        echo "Update the existing bootstrap config or unset PRISM_CONFIG_PATH so ./start.sh can seed a launcher-local bootstrap file."
        exit 1
    fi

    if ! is_launcher_compatible_backend_host "$EFFECTIVE_BACKEND_HOST"; then
        echo "Error: launcher mode expects a local backend bind host, but bootstrap config resolves host '$EFFECTIVE_BACKEND_HOST'."
        echo "Update the existing bootstrap config or unset PRISM_CONFIG_PATH so ./start.sh can seed a launcher-local bootstrap file."
        exit 1
    fi
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

build_backend_binary() {
    (cd "$BACKEND_DIR" && "$BACKEND_GO_BIN" build -o "$BACKEND_BINARY_PATH" ./cmd/prism-backend)
}

parse_database_host_port() {
    local database_url="$1"
    local host
    local port

    if [[ "$database_url" =~ ^postgres(ql)?://([^@/]+@)?(\[[^]]+\]|[^:/?#]+):([0-9]+)([/?#].*)?$ ]]; then
        host="${BASH_REMATCH[3]}"
        port="${BASH_REMATCH[4]}"
        host="${host#[}"
        host="${host%]}"
        printf '%s %s\n' "$host" "$port"
        return 0
    fi

    return 1
}

tcp_port_open() {
    local host="$1"
    local port="$2"

    if command -v nc >/dev/null 2>&1; then
        nc -z "$host" "$port" >/dev/null 2>&1
        return
    fi

    (exec 3<>"/dev/tcp/$host/$port") >/dev/null 2>&1
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

    database_url="$EFFECTIVE_DATABASE_URL"
    if [ "$DATABASE_URL_FROM_ENV" = false ] && [ ! -f "$PRISM_CONFIG_PATH" ]; then
        echo "DATABASE_URL is not set; using default seed input: $DATABASE_URL"
    fi
    echo "Backend database URL: $database_url"

    if [[ "$database_url" =~ ^postgresql\+asyncpg:// ]]; then
        echo "Error: DATABASE_URL uses the retired Python-era asyncpg DSN format."
        echo "Use postgres:// or postgresql:// instead."
        echo "Current value: $database_url"
        exit 1
    fi

    case "$database_url" in
        [Pp][Oo][Ss][Tt][Gg][Rr][Ee][Ss]://*|[Pp][Oo][Ss][Tt][Gg][Rr][Ee][Ss][Qq][Ll]://*)
            ;;
        *)
            echo "Error: DATABASE_URL must point to PostgreSQL using postgres:// or postgresql://."
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
    echo "Start the database Prism is configured to use, then retry."
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
    kill_running_on_port "$EFFECTIVE_BACKEND_PORT" "backend"
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
    if [[ -n "$EFFECTIVE_BACKEND_PORT" ]]; then
        kill_running_on_port "$EFFECTIVE_BACKEND_PORT" "backend"
    fi
    kill_running_on_port "$FRONTEND_PORT" "frontend"
    kill_running_on_port "$DATABASE_PORT" "database"
    [[ -n "${LAUNCHER_TMP_DIR:-}" ]] && rm -rf "$LAUNCHER_TMP_DIR"
    echo "Done."
}
trap cleanup EXIT INT TERM

configure_local_frontend_backend_integration
configure_launcher_bootstrap_seed_inputs
configure_bootstrap_startup_contract

if [ "$BOOTSTRAP_CONFIG_PATH_FROM_ENV" = false ]; then
    echo "PRISM_CONFIG_PATH is not set; using launcher-local bootstrap config: $PRISM_CONFIG_PATH"
fi

# --- Backend setup ---
if ! command -v "$BACKEND_GO_BIN" >/dev/null 2>&1; then
    echo "Error: $BACKEND_GO_BIN is required to build and run the backend."
    exit 1
fi

if [ "$START_FRONTEND" = true ] && ! command -v "$FRONTEND_PNPM_BIN" >/dev/null 2>&1; then
    echo "Error: $FRONTEND_PNPM_BIN is required to manage the frontend."
    exit 1
fi

echo "Building backend with Go..."
build_backend_binary
ensure_bootstrap_config_exists
resolve_effective_backend_startup_settings
ensure_launcher_backend_binding_matches_expectations
kill_existing_instances
start_database_container
wait_for_database_container
ensure_backend_database_ready

if [ "$START_FRONTEND" = true ]; then
    # --- Frontend setup ---
    if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
        echo "Installing frontend dependencies..."
        (cd "$FRONTEND_DIR" && "$FRONTEND_PNPM_BIN" install)
    fi
fi

# --- Start backend ---
echo "Starting backend on port $EFFECTIVE_BACKEND_PORT..."
run_backend_with_bootstrap_config "$BACKEND_BINARY_PATH" &
BACKEND_PID=$!

if [ "$START_FRONTEND" = true ]; then
    # --- Start frontend ---
    # Frontend keeps browser traffic same-origin and proxies backend routes locally.
    echo "Starting frontend on port $FRONTEND_PORT..."
    (cd "$FRONTEND_DIR" && env -u VITE_API_BASE PRISM_VITE_PROXY_ENABLED=1 \
        PRISM_VITE_PROXY_TARGET="http://localhost:$EFFECTIVE_BACKEND_PORT" \
        "$FRONTEND_PNPM_BIN" exec vite --port "$FRONTEND_PORT" --host) &
    FRONTEND_PID=$!
fi

echo ""
echo "========================================="
echo "  LLM Proxy Gateway"
echo "  Mode:     $MODE"
echo "  Backend:  http://localhost:$EFFECTIVE_BACKEND_PORT"
echo "  Config:   $PRISM_CONFIG_PATH"
if [ "$START_FRONTEND" = true ]; then
    echo "  Frontend: http://localhost:$FRONTEND_PORT"
else
    echo "  Frontend: disabled (headless mode)"
fi
echo "  API Docs: http://localhost:$EFFECTIVE_BACKEND_PORT/docs"
echo "========================================="
echo ""

wait
