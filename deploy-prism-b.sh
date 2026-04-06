#!/usr/bin/env bash
set -euo pipefail

REMOTE_HOST="capy"
REMOTE_DIR="orange_work/curse"
DEPLOY_NAME="prism-b"

usage() {
    cat <<'EOF'
Usage: ./deploy-prism-b.sh

Runs the remote Prism deploy flow on capy:
  1. cd orange_work/curse
  2. ./deploy.sh start prism-b
  3. if that fails, ./deploy.sh force prism-b
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

if [[ "$#" -ne 0 ]]; then
    usage
    exit 1
fi

require_command ssh

log "Deploying $DEPLOY_NAME on $REMOTE_HOST:$REMOTE_DIR"

ssh "$REMOTE_HOST" 'bash -s' -- "$REMOTE_DIR" "$DEPLOY_NAME" <<'EOF'
set -euo pipefail

remote_dir="$1"
deploy_name="$2"

cd "$remote_dir"

echo "==> Running ./deploy.sh start ${deploy_name}"
if ./deploy.sh start "$deploy_name"; then
    exit 0
fi

echo "==> ./deploy.sh start ${deploy_name} failed; retrying with ./deploy.sh force ${deploy_name}"
./deploy.sh force "$deploy_name"
EOF

log "Remote deploy completed"
