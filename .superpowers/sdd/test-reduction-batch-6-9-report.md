## Scope

- Batch 6.9 target: `backend/tests/contract/s11_management_contract_test.go`
- Changed file: `backend/tests/contract/s11_management_contract_test.go`
- Local ignored report: `.superpowers/sdd/test-reduction-batch-6-9-report.md`
- No production code changed.
- Kept `TestAuditSettingsRouteContractProfileScope` untouched; the parity block remains intact.

## Counts

- LOC before: `1302`
- LOC after: `1068`
- `func Test` count before: `13`
- `func Test` count after: `13`
- Net LOC reduction: `234`

## Coverage Mapping

- `TestCostingSettings`
  - Replaced repeated GET/PUT/GET decode boilerplate with shared JSON request helpers.
  - Coverage kept for default payload, invalid model/endpoint mapping, normalization, persistence.
- `TestTimezoneSettings`
  - Reused shared JSON request helper for GET/PUT assertions.
  - Coverage kept for default/null, trim, blank-to-null.
- `TestAuditSettings`
  - Reused shared JSON request helper plus `auditSettingsRequest` / `auditSetting`.
  - Coverage kept for defaults, normalization, persistence, profile-header override semantics, DB row assertions, invalid payload cases.
- Route contract tests
  - `TestAuditSettingsRouteContractProfileScope` left unchanged.
  - The other two route-contract tests now reuse `assertManagementRouteContract`.
- `TestGlobalLogRetentionSettingsAndJobs`
  - Added and used `putThenGetJSON`.
  - Replaced repeated PUT/GET/JSON decode blocks with `putThenGetJSON` + `assertLogRetentionPayload`.
  - Coverage kept for default/nulls, invalid retention day, persist/update, clear-to-null, legacy 404s, maintenance job response schema.
- Load-balance strategy tests
  - Introduced `legacyStrategyPayload` and collapsed repeated Ban Policy payload walls.
  - Folded the four invalid create payload checks in `TestLoadbalanceStrategies` into a table.
  - Coverage kept for GET detail payload, removed-shape rejection, create/update/delete, attached-model delete conflict, defaults idempotency/conflict.
- Config rule tests
  - Header blocklist and user-agent rule CRUD now share `runConfigRuleCRUDContract`.
  - Coverage kept for system rule discovery, invalid payload, create/get/update, system toggle, system immutability, custom delete, system delete 404.

## Merged Tests/Helpers

- Added helpers:
  - `requestJSONStatus[T any]`
  - `putThenGetJSON`
  - `assertManagementRouteContract`
  - `assertLogRetentionPayload`
  - `legacyStrategyPayload`
  - `auditSettingsRequest`
  - `auditSetting`
  - `runConfigRuleCRUDContract`
  - `assertMapFields`
- Removed duplicated per-test PUT/GET decode boilerplate and repeated config-rule CRUD scaffolding.
- Preserved top-level test count; compression came from helper reuse and table-driven invalid cases.

## Verification

1. `cd backend && go test ./tests/contract -run 'S11|Management|LogRetention|Route|Settings|Jobs' -count=1`
   - Pass
2. `cd backend && go test ./tests/contract -count=1`
   - Pass
3. `cd backend && go test -count=1 ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...`
   - Partial pass
   - `tests/contract`: pass
   - `tests/runtime`: pass
   - `tests/priority/...`: pass
   - `tests/integration`: fail because `./start.sh` refused to start on port `8000`
   - Observed external process on port `8000`: PID `84017`, `Visual Studio Code.app ... Code Helper (Plugin)`
4. `cd backend && go test -count=1 ./internal/... ./cmd/...`
   - Pass
5. `cd backend && go build ./cmd/prism-backend`
   - Pass
6. Counts / ignore
   - `wc -l backend/tests/contract/s11_management_contract_test.go` -> `1068`
   - `rg -c "func Test" backend/tests/contract/s11_management_contract_test.go` -> `13`
   - `git check-ignore -v .superpowers/sdd/test-reduction-batch-6-9-report.md`
     - `.gitignore:39:.superpowers`

## Concerns

- Batch target was `~850` LOC; this pass reached `1068`. The main reduction landed in JSON round-trip boilerplate, config-rule CRUD scaffolding, and repeated Ban Policy payloads, but it did not hit the requested target.
- Full backend regression is still blocked by external port `8000` occupancy, not by this diff.
