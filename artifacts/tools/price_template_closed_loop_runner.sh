#!/bin/sh
set -eu

runner_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_root=$(CDPATH= cd -- "$runner_dir/../.." && pwd)
case_id=${1:-}

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
    cd "$project_root"
    exec python3 /Users/qingli/.codex/plugins/cache/coachpo/project-workflow/0.9.0+codex.20260817212547/skills/write-project-docs/scripts/validate_project_docs.py --strict . --language en
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
    exec pnpm run test:e2e
    ;;
  *)
    echo "unknown closed-loop case: $case_id" >&2
    exit 2
    ;;
esac
