# Task 24 Report

## Status

DONE_WITH_CONCERNS

## Summary

- Folded `backend/internal/targetcompat` into `backend/internal/domain/modelrouting`.
- Preserved persisted target value `target_type = "connection"`.
- Replaced management model route target glossary calls with `modelrouting`.
- Inlined the owner-scoped connection route path in the connection mutation rejection text.
- Renamed `backend/internal/providercompat` to `backend/internal/providerauth`, including package declarations, file names, imports, and tests.
- Updated backend/docs ownership references so active backend/docs/root AGENTS scans no longer mention the retired package names.

## Verification

- `rg -n "targetcompat|providercompat" backend/ docs/ AGENTS.md`
  - Passed; no matches.
- `cd backend && go test ./internal/providerauth ./internal/domain/modelrouting ./internal/httpapi/management/models ./internal/httpapi/management/connections ./internal/httpapi/runtime`
  - Passed.
- `cd backend && go build ./cmd/prism-backend`
  - Passed.
- `cd backend && go test ./internal/providerauth ./internal/domain/modelrouting ./internal/httpapi/management/models ./internal/httpapi/management/connections ./internal/httpapi/management/settings ./internal/httpapi/runtime`
  - Failed only in `internal/httpapi/management/settings`: `TestAuditSettingsRouteDefaultsReplacementValidationRollback` expected profile 2 but handler returned profile 1. The Task 24 diff in settings is import/package rename only.
- `cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...`
  - Failed in existing broader suites:
    - `tests/integration`: migration 000009 failed because `public.request_logs` did not exist.
    - `tests/runtime`: route matrix expected 11 registered POST operations but saw 9; request-log fixture drift; profile-scope insert parameter type issue.
    - `tests/priority/cache`: source-text expectation for `LoadFreshActiveRuntimePlan` failed.
  - `tests/contract` and most priority packages passed.

## Concerns

- Broader backend suites are not green on this working tree/base. The observed failures do not line up with Task 24 behavior changes, which are package folding/renaming only.
