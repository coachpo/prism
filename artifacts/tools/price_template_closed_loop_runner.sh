#!/bin/sh
set -eu

runner_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_root=$(CDPATH= cd -- "$runner_dir/../.." && pwd)
case_id=${1:-}
PRISM_PLAYWRIGHT_PROCESS_PATCH="$runner_dir/playwright_closed_loop_process_patch.cjs"
PRISM_GO_WORKSPACE=/Users/qingli/go
PRISM_GO_BUILD_CACHE=/Users/qingli/Library/Caches/go-build
export GOPATH="$PRISM_GO_WORKSPACE"
export GOMODCACHE="$PRISM_GO_WORKSPACE/pkg/mod"
export GOCACHE="$PRISM_GO_BUILD_CACHE"
export PLAYWRIGHT_BROWSERS_PATH=/Users/qingli/Library/Caches/ms-playwright

case "$case_id" in
  backend-build)
    cd "$project_root/backend"
    exec go build ./cmd/prism-backend
    ;;
  frontend-build)
    cd "$project_root/frontend"
    exec pnpm run build
    ;;
  backend-unit)
    cd "$project_root/backend"
    exec go test ./internal/... ./cmd/...
    ;;
  frontend-vitest)
    cd "$project_root/frontend"
    exec pnpm exec vitest run
    ;;
  frontend-lint)
    cd "$project_root/frontend"
    exec pnpm run lint
    ;;
  docs-contract)
    PRISM_DOCS_VALIDATION_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/prism-docs-validation.XXXXXX")
    trap 'rm -rf -- "$PRISM_DOCS_VALIDATION_ROOT"' EXIT HUP INT TERM
    git -C "$project_root" archive HEAD | tar -x -C "$PRISM_DOCS_VALIDATION_ROOT"
    python3 /Users/qingli/.codex/plugins/cache/coachpo/project-workflow/0.9.0+codex.20260817212547/skills/write-project-docs/scripts/validate_project_docs.py --strict "$PRISM_DOCS_VALIDATION_ROOT" --language en
    ;;
  typed-pricing-roleplay)
    cd "$project_root/backend"
    exec go test -count=1 ./tests/runtime -run '^TestRuntimeTypedPricingOperationMatrix$'
    ;;
  backend-regression)
    cd "$project_root/backend"
    exec go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
    ;;
  frontend-lib)
    cd "$project_root/frontend"
    exec pnpm run test:lib
    ;;
  frontend-e2e)
    cd "$project_root/frontend"
    export NODE_OPTIONS="--require=$PRISM_PLAYWRIGHT_PROCESS_PATCH"
    exec pnpm run test:e2e
    ;;
  *)
    echo "unknown closed-loop case: $case_id" >&2
    exit 2
    ;;
esac
