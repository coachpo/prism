#!/usr/bin/env bash
set -euo pipefail

REMOTE_HOST="capy"
REMOTE_DIR="orange_work/curse"

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

require_command ssh

log "Forwarding arguments to $REMOTE_HOST:$REMOTE_DIR/./deploy.sh"

ssh "$REMOTE_HOST" 'bash -s' -- "$REMOTE_DIR" "$@" <<'EOF'
set -euo pipefail

remote_dir="$1"
shift

cd "$remote_dir"

if [[ "$#" -eq 0 ]]; then
    echo "==> Running ./deploy.sh"
else
    echo "==> Running ./deploy.sh $*"
fi

./deploy.sh "$@"
EOF

log "Remote deploy completed"
