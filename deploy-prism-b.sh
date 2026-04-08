#!/usr/bin/env bash
set -euo pipefail

REMOTE_HOST="capy"
REMOTE_DIR="orange_work/curse"
DEPLOY_NAME="prism-b"

usage() {
    cat <<'EOF'
Usage: ./deploy-prism-b.sh [--version <tag>]

Runs the remote Prism deploy flow on capy:
  1. cd orange_work/curse
  2. ./deploy.sh start prism-b [--version <tag>]
  3. if that fails, ./deploy.sh force prism-b [--version <tag>]
EOF
}

log() {
    echo "==> $*"
}

fail() {
    echo "Error: $*" >&2
    exit 1
}

require_command() {
    local command_name="$1"
    if ! command -v "$command_name" >/dev/null 2>&1; then
        fail "Missing required command: $command_name"
    fi
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

version=""

while [[ "$#" -gt 0 ]]; do
    case "$1" in
        --version)
            if [[ "$#" -lt 2 || -z "${2:-}" ]]; then
                fail "Missing value for --version"
            fi
            version="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            usage
            exit 1
            ;;
    esac
done

require_command ssh

log "Deploying $DEPLOY_NAME on $REMOTE_HOST:$REMOTE_DIR"

ssh "$REMOTE_HOST" 'bash -s' -- "$REMOTE_DIR" "$DEPLOY_NAME" "$version" <<'EOF'
set -euo pipefail

remote_dir="$1"
deploy_name="$2"
version="$3"

deploy_args=("$deploy_name")

if [[ -n "$version" ]]; then
    deploy_args+=(--version "$version")
fi

cd "$remote_dir"

echo "==> Running ./deploy.sh start ${deploy_args[*]}"
if ./deploy.sh start "${deploy_args[@]}"; then
    exit 0
fi

echo "==> ./deploy.sh start ${deploy_args[*]} failed; retrying with ./deploy.sh force ${deploy_args[*]}"
./deploy.sh force "${deploy_args[@]}"
EOF

log "Remote deploy completed"
