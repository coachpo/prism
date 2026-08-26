# BACKEND MODELS.DEV DOMAIN KNOWLEDGE BASE

## OVERVIEW

`internal/domain/modelsdev/` owns the HTTP-neutral boundary for the fixed models.dev catalog: restricted fetching, schema validation, lossless numeric parsing, exact offering matching, bounded candidate search, and fail-closed pricing-plan mapping. It does not own management routes or runtime routing.

## STRUCTURE

```text
modelsdev/
├── client.go                    # HTTPS-only client, same-origin redirects, timeout, body cap, ETag, singleflight
├── catalog.go                   # JSON schema parsing, validation, and canonical price literals
├── match.go                     # Canonical provider mapping, exact matches, bounded candidates
├── pricing.go                   # Fail-closed standard/tiered price-plan mapping
├── modelsdev_testdata_test.go   # Static catalog fixture and fixture loader
├── modelsdev_client_test.go     # Client, schema transport, redirect, ETag, and concurrency tests
├── modelsdev_schema_test.go     # Schema and lossless numeric/canonical-price tests
├── modelsdev_matching_test.go   # Matching and candidate-scope tests
└── modelsdev_pricing_test.go    # Pricing-plan and incompatibility tests
```

## WHERE TO LOOK

- Restricted remote transport: `client.go` (`NewClient`, `Fetch`, `Snapshot`); management callers must fetch before transactions and may use only `Snapshot` inside commit transactions.
- Catalog schema and numeric fidelity: `catalog.go`; schema failures remain unavailable and never become partial data.
- Provider/model matching: `match.go`; auto-binding requires one exact canonical-provider match, while bounded search supports explicit manual selection.
- Catalog pricing mapping: `pricing.go`; unsupported, non-USD, incomplete, audio, tier-conflicting, or specialty-mismatched data stays non-committable.
- Test ownership: `modelsdev_client_test.go`, `modelsdev_schema_test.go`, `modelsdev_matching_test.go`, `modelsdev_pricing_test.go`, and the static fixture owner `modelsdev_testdata_test.go`.

## CONVENTIONS

- Keep this package HTTP-neutral and management-only. No route parsing, profile resolution, database transactions, runtime cache invalidation, or provider routing belongs here.
- `Fetch` is the only network entrypoint. Enforce HTTPS, pinned-origin redirects, the whole-request timeout, the bounded body read, ETag/304 reuse, and singleflight behavior there.
- `Snapshot` and `CurrentRevision` are memory-only. A caller must never fetch remotely from inside a database transaction.
- Preserve catalog price literals losslessly through `json.Number`; explicit zero prices remain configured values, not missing data.
- Keep matching exact and deterministic. Aggregator providers are never auto-selected when the family mapping does not name them.
- Keep `BuildPricePlan` fail closed and management-only; it must not affect `api_family`, capability equality, routing, or runtime snapshots.

## ANTI-PATTERNS

- Do not add management handlers, SQL, profile scope, cache invalidation, or runtime routing decisions here.
- Do not broaden the allowed catalog origin, redirect policy, timeout, body limit, or auto-match provider mapping.
- Do not convert unavailable, ambiguous, unsupported, or incomplete catalog evidence into a committable plan.
