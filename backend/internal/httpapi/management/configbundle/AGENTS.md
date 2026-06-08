# BACKEND MANAGEMENT CONFIG BUNDLE KNOWLEDGE BASE

## OVERVIEW
`management/configbundle/` owns Prism's profile bundle and vendor-catalog export/import surface. It handles preview tokens, bundle-fingerprint validation, secret encryption/decryption, and context overflow promotion target validation. Cross-cutting runtime-cache and dashboard invalidation for successful imports is owned by platform HTTP management mutation middleware.

## STRUCTURE
```text
configbundle/
├── service.go        # Service wiring, route mounting, bundle secret key ownership
├── routes.go         # Export / preview / import route handlers
├── import.go         # Bundle parsing, validation, preview, promotion-target checks, and import execution
├── store.go          # Bundle export assembly and import persistence helpers
├── types.go          # Request/response payloads and bundle models
├── import_types.go   # Import-side helper types and diff models
├── preview_tokens.go # Preview-token issuance and validation
├── crypto.go         # Bundle secret encryption/decryption helpers
└── *_test.go         # Import, preview-token, store, and promotion-target coverage
```

## WHERE TO LOOK
- Route mounting and bundle service wiring: `service.go`, `routes.go`
- Profile bundle export, preview, and import flow: `routes.go`, `import.go`, `store.go`
- Vendor catalog export/preview/import flow: `routes.go`, `import.go`, `store.go`
- Context overflow promotion target import/export validation: `import.go`, `store.go`, `promotion_target_test.go`
- Preview-token issuance and validation: `preview_tokens.go`
- Bundle secret encryption/decryption and key derivation: `crypto.go`, `service.go`
- Frontend consumers: `../../../../../frontend/src/pages/settings/useConfigBackupData.ts`, `../../../../../frontend/src/pages/settings/useVendorManagementData.ts`, `../../../../../frontend/src/lib/configImportValidation.ts`

## CONVENTIONS
- Keep profile bundle and vendor catalog flows separate from startup bootstrap config and from log-retention or sidecar ownership.
- Keep profile bundles on the v3 contract with top-level private connections, exactly-one-owner connection refs in ordered model access targets, nullable `context_overflow_promotion_target_id`, and explicit Ban Policy fields: `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, and `ban_mode` values `off`, `temporary`, or `until_reset`.
- Keep context overflow promotion targets import-validated against imported model IDs, same `api_family`, enabled non-facade targets, and larger usable terminal context.
- Keep preview-before-import semantics explicit; validated imports should require the preview token path that the backend issued for that exact bundle fingerprint.
- Keep bundle-secret handling explicit and transactional; do not bury encryption/decryption in page code or shared settings helpers.
- Keep effective-profile resolution inside this package; keep cross-cutting import side effects in the platform HTTP management mutation middleware instead of reintroducing package-local hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not bypass preview-token validation or import-envelope checks.
- Do not mix bootstrap config, runtime proxy behavior, or sidecar control-plane state into bundle flows.
- Do not accept promotion targets outside the imported profile graph or silently drop invalid `context_overflow_promotion_target_id` values.
- Do not expose plaintext bundle secrets in preview or import responses.
- Do not split one bundle family across multiple backend client surfaces without a real backend-boundary change.
