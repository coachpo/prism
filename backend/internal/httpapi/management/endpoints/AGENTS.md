# BACKEND MANAGEMENT ENDPOINTS KNOWLEDGE BASE

## OVERVIEW
`management/endpoints/` owns Endpoint CRUD, direct-reference reads, one-time verification and orphan cleanup under `/api/endpoints*` pinned to Default profile id `1`. Endpoints are reusable upstream base URLs with at-rest-encrypted API keys, a plaintext-derived display fingerprint and independent key identity time. `X-Profile-Id` may be accepted but is ignored; storage `profile_id` columns remain.

## STRUCTURE
```text
endpoints/
├── service.go          # Service construction and endpoint route mounting
├── routes.go           # CRUD (create/update/duplicate/list/dropdown), typed 422 fields
├── store.go            # Endpoint persistence, deterministic lower(name) list order
├── types.go            # Endpoint request/response DTO (no masked/position)
├── references_types.go # Reference summary/detail/cursor/verify DTOs
├── references_store.go # Canonical direct-reference query, summary/hash derivation, signed cursor
├── references_routes.go# Batch summaries, single detail pages, lock-time delete, orphan cleanup
└── verify.go           # One-time family-aware metadata probe, classification, redaction
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Endpoint CRUD and secret-metadata write semantics (same-key preserve, rotation, no-op update, duplicate key copy): `routes.go`, `endpointdomain`.
- Direct-reference truth shared by batch/single/delete: `references_store.go` (`canonicalReferenceQuery`, `deriveCanonicalSet`, `snapshotHash`).
- Signed opaque cursor and stale/mismatch behavior: `references_store.go`, `references_routes.go`.
- One-time verify (OpenAI/Anthropic/Gemini metadata probes, outcome classification, zero side effects): `verify.go`.
- Orphan cleanup race handling and typed `409` blockers: `references_routes.go`.

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep endpoint writes pinned to Default profile id `1`.
- Direct references are the only deletion/filter truth: never filter blockers by enable state, never use recursive model reachability, never collapse unknown/stale to zero, and treat orphan connections as first-class blockers.
- Keep API keys encrypted at rest; raw keys never enter read responses, errors, logs, audit rows, URLs or evidence. Fingerprints come from plaintext via the domain-separated HMAC helper in `endpointdomain`; never hash ciphertext.
- Endpoint display order is `lower(name), name, id`; there is no `position` column, route, or reorder behavior. The authoritative target order stays `model_access_targets.position`.
- `config_revision` bumps only on normalized base URL or key-identity change; name-only edits touch `updated_at` only; fully unchanged updates are no-ops.
- Verify is read-only and non-persistent: no request logs, usage, audit, loadbalance state, health fields or Endpoint mutations; no cross-origin credential forwarding; bounded body/error summaries.
- Inline endpoint creation for connection forms belongs to `connections/`, which reuses the same `endpointdomain` secret-metadata helper.

## LLM UPSTREAM MATRIX
- When endpoint base URL, auth, or provider-facing behavior changes, evaluate OpenAI, Anthropic, and Gemini endpoint expectations, and cover both streaming and non-streaming operation shapes where runtime behavior is affected.

## ANTI-PATTERNS
- Do not return plaintext API keys, ciphertext hashes, key suffixes or the masked constant from endpoint responses.
- Do not re-add endpoint `position`/move routes, columns, indexes or normalization helpers.
- Do not delete endpoints while canonical direct references still exist; the lock-time recheck is authoritative.
- Do not store verify results in health/telemetry/audit or send generation probes.
- Do not use `updated_at` as key-rotation evidence.
- Do not move inline endpoint creation for connection forms out of `connections/`.

## UX-UPGRADE SURFACES

- `POST /api/endpoints/references/batch` returns compact direct-reference summaries. The typed `409 endpoint_in_use` blocker carries the canonical `endpoint_id`, summary and bounded `reference_page` snapshot used by the detail route; if the locked recheck finds no references, delete proceeds. References never include endpoint API keys or header/parameter values.
