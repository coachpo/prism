# BACKEND MANAGEMENT CONFIG BUNDLE KNOWLEDGE BASE

## OVERVIEW
`management/configbundle/` owns Prism's profile bundle and vendor-catalog export/import surface. It handles preview tokens, bundle-fingerprint validation, secret encryption/decryption, and replace-mode import validation. Cross-cutting runtime-cache and dashboard invalidation for successful imports is owned by platform HTTP management mutation middleware.

## STRUCTURE
```text
configbundle/
├── service.go        # Service wiring, route mounting, bundle secret key ownership
├── routes.go         # Export / preview / import route handlers
├── import.go         # Bundle parsing, validation, preview, and import execution
├── store.go          # Bundle export assembly and import persistence helpers
├── types.go          # Request/response payloads and bundle models
├── import_types.go   # Import-side helper types and diff models
├── preview_tokens.go # Preview-token issuance and validation
├── crypto.go         # Bundle secret encryption/decryption helpers
└── *_test.go         # Import, preview-token, and store coverage
```

## WHERE TO LOOK
- Route mounting and bundle service wiring: `service.go`, `routes.go`
- Profile bundle export, preview, and import flow: `routes.go`, `import.go`, `store.go`
- Preview-token issuance and validation: `preview_tokens.go`
- Bundle secret encryption/decryption and key derivation: `crypto.go`, `service.go`
- Frontend consumers: `../../../../../frontend/src/pages/settings/useConfigBackupData.ts`, `../../../../../frontend/src/lib/configImportValidation.ts`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep profile bundles on the v3 contract with top-level private connections, exactly-one-owner connection refs in ordered model access targets, `profile_settings.audit_api_family_settings` as the three-family full-replacement audit policy, and explicit Ban Policy fields: `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, and `ban_mode` values `off`, `temporary`, or `until_reset`.
- Reject obsolete access-target `weight` and `target_priority` keys during import and preview instead of upgrading or dropping them silently.
- Keep preview-before-import semantics explicit; validated imports should require the preview token path that the backend issued for that exact bundle fingerprint.
- Keep bundle-secret handling explicit and transactional; do not bury encryption/decryption in page code or shared settings helpers.
- Keep effective-profile resolution inside this package; keep cross-cutting import side effects in the platform HTTP management mutation middleware instead of reintroducing package-local hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not bypass preview-token validation or import-envelope checks.
- Do not expose plaintext bundle secrets in preview or import responses.
- Do not split one bundle family across multiple backend client surfaces without a real backend-boundary change.
