# Plan: Close Remaining Usage/Costing Gaps on Latest Codebase

## Summary
Latest code already includes big pieces (OpenAI host-agnostic `include_usage`, v4 config, renamed cache fields), but a few correctness gaps remain. This plan finalizes behavior so token accounting and pricing match the intended contract across OpenAI-compatible, Anthropic, and Gemini.

## Current gaps to resolve
1. Anthropic top-level `usage.cache_read_input_tokens` is not read in fallback parsing (`backend/app/services/stats_service.py:197`).
2. Missing special-token fields are still persisted as `null` when usage exists; target behavior is `0`.
3. `MAP_TO_OUTPUT` fallback is not applied to missing `reasoning_price` (currently becomes `0`) (`backend/app/services/costing_service.py:210`, `backend/app/services/costing_service.py:264`).
4. Legacy field footprints still exist in ORM/migrations and one frontend type key mismatch remains (`frontend/src/lib/types.ts:205`).
5. Tests pass, but do not fully enforce the new semantics for all provider edge-cases.

## Public API / interface changes (final contract)
- Keep and enforce only these request-log keys in API responses:
  - `cache_read_input_tokens`
  - `cache_creation_input_tokens`
  - `cache_read_input_cost_micros`
  - `cache_creation_input_cost_micros`
  - `pricing_snapshot_cache_read_input`
  - `pricing_snapshot_cache_creation_input`
  - `pricing_snapshot_missing_special_token_price_policy`
- Keep endpoint policy field:
  - `missing_special_token_price_policy`
- Keep config format:
  - export/import `version: 4` only.
- Enforce token-nullability rule:
  - No usage block: token fields `null`.
  - Usage block present but special fields missing: special fields `0`.

## Implementation plan

### 1) Parser correctness pass (Anthropic + Gemini + generic usage)
- Update `_extract_special_usage` to read all upstream names at both nested and top-level:
  - include `usage.cache_read_input_tokens` directly.
- Add normalization step after extraction:
  - if `usage` object exists, coerce missing special fields to `0`.
  - if no `usage` object exists, keep `null`.
- Keep OpenAI-compatible and Gemini extraction paths intact, only tightening missing-field handling.

### 2) Costing semantics finalization (price-only policy)
- In `compute_cost_fields`:
  - keep token counts as-reported (never copy from `output_tokens`).
  - treat missing token counts as `0` for cost math only.
- Implement effective price fallback for **all special prices**:
  - cache read, cache creation, reasoning.
  - `MAP_TO_OUTPUT` => fallback to `output_price`.
  - `ZERO_COST` => fallback to `0`.
- Preserve unpriced behavior when there is no usage at all (`MISSING_TOKEN_USAGE`).

### 3) OpenAI-compatible usage behavior verification hardening
- Keep current host-agnostic stream-option injection.
- Add regression tests to ensure:
  - third-party OpenAI-compatible hosts preserve `stream_options`;
  - `include_usage=true` is injected for streaming OpenAI requests regardless of host;
  - if upstream still returns no usage, logs remain null/0 by contract (not fabricated from other fields).

### 4) Legacy-field cleanup pass (code-level)
- Remove remaining old-name references from runtime code paths where still present.
- Fix frontend type drift (`pricing_snapshot_policy` -> `pricing_snapshot_missing_special_token_price_policy`).
- Keep DB physical legacy columns untouched for now (hard-reset strategy), but stop reading/writing them in active code.

### 5) Docs + contract sync
- Align docs with final enforced semantics:
  - “price policy affects only prices, never token counts.”
  - usage-null vs usage-present-missing-field behavior.
- Ensure examples in API/data-model docs use final field names only.

## Test plan (must-pass)
1. Anthropic JSON usage with top-level `cache_read_input_tokens` parses correctly.
2. Usage present + missing special fields yields `0` (not `null`) for special token counts.
3. No usage block yields `null` token fields.
4. `MAP_TO_OUTPUT` applies to missing reasoning/cache prices (snapshot reflects fallback).
5. `ZERO_COST` with missing special prices produces `0` special costs.
6. Special token counts are never substituted from output token counts.
7. OpenAI-compatible streaming injects `include_usage` without host checks.
8. Frontend typecheck/build confirms renamed snapshot policy field and stats rendering.

## Rollout / validation
1. Run backend tests (`pytest`) and frontend lint/build.
2. Smoke test one model per provider (OpenAI-compatible/new-api, Anthropic, Gemini).
3. Verify request logs for:
   - explicit zeroes vs nulls,
   - no Out/Cached/Reasoning artificial copying,
   - correct fallback pricing snapshots.

## Assumptions / defaults
- Keep config import/export as v4-only.
- Keep hard-reset strategy for renamed storage fields (no historical backfill).
- Keep legacy DB columns physically present for now, but not used by active runtime/API paths.
