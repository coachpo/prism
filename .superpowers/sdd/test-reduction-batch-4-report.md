# Batch 4 Report

## Scope

- Branch: `codex/prism-core-simplification`
- Batch brief: `.superpowers/sdd/test-reduction-batch-4-brief.md`
- Production code changed: none

## Deletion Coverage Table

| File | Prior test count | Outcome | Coverage destination |
| --- | ---: | --- | --- |
| `backend/internal/httpapi/management/endpoints/routes_test.go` | 2 | deleted | `backend/tests/contract/endpoint_contract_test.go` now owns masked secret regression on list/read shape and the dependent routing/pricing/health freeze assertion, plus existing CRUD/delete/reorder/duplicate coverage |
| `backend/internal/httpapi/management/loadbalance/routes_test.go` | 2 | deleted | `backend/tests/contract/s11_management_contract_test.go` already owns strategy CRUD/defaults/delete protection and route-contract coverage |
| `backend/internal/httpapi/management/settings/routes_test.go` | 5 | trimmed to 4 pure unit tests | route/DB coverage already lives in `backend/tests/contract/s11_management_contract_test.go`; package-local validation/default helpers remain |
| `backend/internal/httpapi/management/models/promotion_target_test.go` | 1 | deleted | obsolete access-target rejection and flat target behavior already live in `backend/tests/contract/model_contract_test.go` |
| `backend/internal/httpapi/management/models/store_test.go` | 1 | deleted | flat access-target persistence / obsolete-column absence already covered by `backend/tests/contract/model_contract_test.go` plus migration guardrails in `backend/tests/integration/migrations_test.go` |
| `backend/internal/httpapi/management/connections/routes_test.go` | 8 | trimmed to 7 pure unit tests | pricing-template import upsert / validation / unknown-field coverage moved to `backend/tests/contract/connection_s10_contract_test.go`; package-local helper validation tests remain |
| `backend/internal/httpapi/management/models/openai_accepted_format_test.go` | 5 | deleted | OpenAI accepted-format validation, removed context-field rejection, list/detail nested target exposure, and `/api/models/by-endpoint/{id}` exposure now live in `backend/tests/contract/model_contract_test.go` |
| `backend/internal/httpapi/management/models/test_postgres_harness_test.go` | helper only | deleted | no remaining internal model tests need a Docker/Postgres harness after the contract move |
| `backend/internal/platform/managementjobs/jobs_global_cancel_test.go` | 2 | deleted | direct running-job cancellation coverage moved to `backend/tests/integration/management_audit_stats_phase7_test.go:TestGlobalLogRetentionRunningCancelStore`; queued/routed global cancel coverage was already there |
| `backend/internal/platform/logretention/store_test.go` | 4 | trimmed to 2 pure logic tests | DB-backed horizon / partition reloptions / cutoff deletion / vacuum coverage moved to `backend/tests/integration/logretention_store_test.go`; package-local pure logic tests remain |
| `backend/internal/platform/alerting/outbox_test.go` | 4 | deleted | alert webhook outbox enqueue idempotency, worker delivery, live webhook URL snapshot, and retry/backoff behavior moved to `backend/tests/integration/alerting_outbox_test.go` |
| `backend/internal/platform/lifecycle/production_database_test.go` | 1 | converted to pure package test | keeps Batch 3 worker-registration intent in-package by constructing real services with dummy pools and calling `registerDatabaseBackgroundWorkers` without Docker or database startup |

## Key Test Changes

- Added endpoint contract assertions for:
  - masked endpoint API key on follow-up management reads
  - endpoint updates not mutating dependent routing/pricing/health state
- Added integration coverage for:
  - `alerting.Store.EnqueueTx` idempotency
  - `alerting.Store.RegisterBackgroundWorker` delivery / retry behavior via the shared integration Postgres harness
  - `logretention.Store.EnsurePartitionHorizon`
  - `logretention.Store.DropExpiredPartitions`
  - `logretention.Store.DeleteBoundaryRows`
  - `logretention.Store.VacuumAnalyzePartition`
  - `managementjobs.Store.CancelGlobalLogRetentionJob` on running global jobs
- Added contract coverage for:
  - pricing-template import upsert / all-or-nothing validation / unknown JSON field rejection
  - OpenAI accepted-format validation and by-endpoint exposure on model responses
- Replaced the lifecycle Docker-backed internal test with a pure registration test around `registerDatabaseBackgroundWorkers`

## Verification Evidence

- `printenv DATABASE_URL`
  - exited `1` (unset)
- `cd backend && env -u DATABASE_URL go test ./internal/... ./cmd/...`
  - passed
- `cd backend && go test ./tests/contract`
  - passed
- `cd backend && go test ./tests/integration`
  - passed
- `cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/... && go build ./cmd/prism-backend`
  - passed
- `rg -n "docker|postgres:16-alpine|exec\.Command(Context)?\([^\n]*docker" backend/internal --glob '*_test.go'`
  - exited `1` (no internal Docker harness hits remain)

## Docker Evidence

- `docker ps --format '{{.Names}}' | sort` before `env -u DATABASE_URL go test ./internal/... ./cmd/...`:
  - `prism-a-backend-1`
  - `prism-a-frontend-1`
  - `prism-a-postgres-1`
  - `supabase_auth_qr-order-saas-local`
  - `supabase_db_qr-order-saas-local`
  - `supabase_inbucket_qr-order-saas-local`
  - `supabase_kong_qr-order-saas-local`
  - `supabase_realtime_qr-order-saas-local`
  - `supabase_rest_qr-order-saas-local`
  - `supabase_storage_qr-order-saas-local`
- `docker ps --format '{{.Names}}' | sort` after the internal command matched the same list exactly; no transient `prism-*` DB containers started or remained.

## Batch 4 Fix

- Moved the last Docker-backed internal management/alerting coverage into top-level contract and integration suites.
- Replaced the lifecycle Docker test with a pure worker-registration test against the real `registerDatabaseBackgroundWorkers` helper.
- Verified `backend/internal/*_test.go` is now free of self-starting Docker harnesses.
