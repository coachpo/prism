# BACKEND GATEWAY PROVIDER KNOWLEDGE BASE

## OVERVIEW
`backend/internal/gateway/provider/` owns provider adapters: upstream-native request paths, payload translation, response usage extraction, streaming conversion, and provider capability declarations.

## STRUCTURE
```text
provider/
├── adapter.go, default_adapter.go  # Shared adapter contracts and safe defaults
├── openai/                         # Chat/Responses translation and stream conversion
├── anthropic/                      # Messages/count-token path and usage/stream parsing
└── gemini/                         # GenerateContent/countTokens path and stream parsing
```

## WHERE TO LOOK
- Adapter contract, envelopes, `TranslationMode`, `ConversionCapability`, and `TranslationLoss`: `adapter.go`
- Default no-op behavior and unsupported-operation fallback: `default_adapter.go`
- OpenAI Chat/Responses request, response, stream, reasoning, tool, and content handling: `openai/`
- Anthropic Messages request/response/count-token/stream handling: `anthropic/adapter.go`
- Gemini GenerateContent, streamGenerateContent, and countTokens handling: `gemini/adapter.go`
- Cross-provider expectations: `adapter_boundary_test.go`, `adapter_conformance_test.go`

## CONVENTIONS
- Keep provider-native behavior here. Runtime operation selection stays in `../../httpapi/runtime/operations.go`; route planning stays in `../routing/`.
- Unsupported shapes return explicit adapter errors or unsupported capability results. Do not silently passthrough unknown provider APIs.
- Keep OpenAI Chat/Responses translation loss explicit through `TranslationLoss`; do not hide dropped or mapped fields.
- Keep streaming conversion stateful inside provider stream translators; runtime owns transport and audit capture, not provider-specific SSE grammar.
- Add or update conformance tests when changing adapter interfaces, hook behavior declarations, or translation capability.

## ANTI-PATTERNS
- Do not add provider-specific branches in `gateway/core`, `gateway/routing`, or platform code.
- Do not widen Prism's runtime allowlist from adapters.
- Do not treat `DefaultAdapter` as a production fallback for unsupported provider behavior.
