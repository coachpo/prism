# BACKEND GATEWAY CORE KNOWLEDGE BASE

## OVERVIEW
`gateway/core/` owns provider-agnostic gateway contracts shared by runtime planning, provider adapters, routing, and accounting. It defines envelopes, hook permissions, route/accounting types, classification helpers, and adapter-facing errors without knowing Prism's concrete HTTP operation allowlist.

## STRUCTURE
```text
core/
├── pipeline.go        # Pipeline interfaces and execution contracts
├── envelope.go        # Request/response envelope shapes
├── hooks.go           # Hook permissions, execution records, and clone-safe payload access
├── routing.go         # Route attempts, reasons, and selected target metadata
├── errors.go          # Gateway/provider error contracts
├── classification.go  # Stream and response classification helpers
├── context.go         # Gateway context helpers
├── helpers.go         # Package-internal map/slice/byte clone helpers backing clone-safe access
└── *_test.go          # Core behavior and hook-safety tests
```

## WHERE TO LOOK
- Shared runtime/provider envelopes: `pipeline.go`, `envelope.go`
- Hook phase ordering, body/header permissions, payload cloning, and rejection records: `hooks.go`, `hooks_test.go`
- Route attempt state, canonical route reasons, and selected-target metadata: `routing.go`
- Adapter and runtime-facing errors: `errors.go`, `errors_test.go`
- Classification and context helpers used by runtime bridges: `classification.go`, `context.go`

## CONVENTIONS
- Keep this package provider-agnostic. OpenAI, Anthropic, and Gemini behavior belongs under `../provider/` or runtime operation hooks.
- Keep the concrete supported method/path list in `../../httpapi/runtime/operations.go`; this package only carries generic operation metadata passed to it.
- Keep hook payload access explicit and clone-safe. New hook behavior must declare the minimum read/write permissions it needs.
- Keep route reasons stable across `core`, `../routing/`, runtime observability, and accounting.

## ANTI-PATTERNS
- Do not add provider-specific branches or vendor path parsing here.
- Do not widen Prism's runtime allowlist from core abstractions.
- Do not bypass hook execution records when adding request, response, or stream transformations.
