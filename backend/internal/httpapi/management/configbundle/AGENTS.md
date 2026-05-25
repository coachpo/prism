# BACKEND MANAGEMENT CONFIG BUNDLE KNOWLEDGE BASE

## OVERVIEW
`management/configbundle/` owns Prism's profile bundle and vendor-catalog export/import surface. It handles preview tokens, bundle-fingerprint validation, secret encryption/decryption, and the after-import hook that keeps profile and vendor state aligned.

## STRUCTURE
```text
configbundle/
├── service.go        # Service wiring, route mounting, bundle secret key ownership
├── routes.go         # Export / preview / import route handlers
├── import.go         # Bundle parsing, validation, preview, and import execution
├── types.go          # Request/response payloads and bundle models
├── import_types.go   # Import-side helper types and diff models
├── preview_tokens.go # Preview-token issuance and validation
├── crypto.go         # Bundle secret encryption/decryption helpers
└── service_test.go   # Export/import, preview-token, and secret-handling coverage
```

## WHERE TO LOOK
- Route mounting and bundle service wiring: `service.go`, `routes.go`
- Profile bundle export, preview, and import flow: `routes.go`, `import.go`
- Vendor catalog export/preview/import flow: `routes.go`, `import.go`
- Preview-token issuance and validation: `preview_tokens.go`
- Bundle secret encryption/decryption and key derivation: `crypto.go`, `service.go`
- Frontend consumers: `../../../../../frontend/src/pages/settings/useConfigBackupData.ts`, `../../../../../frontend/src/pages/settings/useVendorManagementData.ts`

## CONVENTIONS
- Keep profile bundle and vendor catalog flows separate from startup bootstrap config and from log-retention or sidecar ownership.
- Keep preview-before-import semantics explicit; validated imports should require the preview token path that the backend issued for that exact bundle fingerprint.
- Keep bundle-secret handling explicit and transactional; do not bury encryption/decryption in page code or shared settings helpers.
- Keep effective-profile resolution and after-import hooks inside this package.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS
- Do not bypass preview-token validation or import-envelope checks.
- Do not mix bootstrap config, runtime proxy behavior, or sidecar control-plane state into bundle flows.
- Do not expose plaintext bundle secrets in preview or import responses.
- Do not split one bundle family across multiple backend client surfaces without a real backend-boundary change.
