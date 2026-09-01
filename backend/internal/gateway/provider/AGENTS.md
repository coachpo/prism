# BACKEND GATEWAY PROVIDER KNOWLEDGE BASE

## OVERVIEW
`backend/internal/gateway/provider/` owns provider adapters: upstream-native request paths, response usage extraction, streaming classification, and provider capability declarations.

## STRUCTURE
```text
provider/
├── adapter.go, default_adapter.go  # Shared adapter contracts and safe defaults
├── openai/                         # Native Chat/Responses handling and usage parsing
│   └── AGENTS.md                   # OpenAI adapter and capability rules
├── anthropic/                      # Messages/count-token path and usage/stream parsing
├── gemini/                         # GenerateContent/countTokens path and stream parsing
└── *_test.go                       # Adapter conformance and provider-native coverage
```

## WHERE TO LOOK
- Adapter contract, request/response envelopes, overflow classification, and usage results: `adapter.go`
- Default no-op behavior and unsupported-operation fallback: `default_adapter.go`
- OpenAI Chat/Responses native request paths, usage extraction, and overflow handling: `openai/AGENTS.md`, `openai/`
- Anthropic Messages request/response/count-token/stream handling: `anthropic/adapter.go`
- Gemini GenerateContent, streamGenerateContent, and countTokens handling: `gemini/adapter.go`
- Cross-provider expectations: `adapter_conformance_test.go`

## CONVENTIONS
- Keep provider-native behavior here. Runtime operation selection stays in `../../httpapi/runtime/operations.go`; route planning stays in `../routing/`.
- Unsupported shapes return explicit adapter errors or unsupported capability results. Do not silently passthrough unknown provider APIs.
- Keep OpenAI Chat Completions and Responses operation-native; wire mismatches are rejected during runtime planning rather than converted here.
- Runtime owns transport, stream terminal parsing, and audit capture; adapters must not widen the operation registry.
- Model-bound provider request DTOs name their wire identity `UpstreamModelID`. Runtime supplies the selected Terminal Target's frozen value per attempt; logical Prism identities remain in routing/telemetry contracts and must not be inferred or substituted here.
- Add or update conformance tests when changing adapter interfaces or hook behavior declarations.

## ANTI-PATTERNS
- Do not add provider-specific branches in `gateway/core`, `gateway/routing`, or platform code.
- Do not widen Prism's runtime allowlist from adapters.
- Do not treat `DefaultAdapter` as a production fallback for unsupported provider behavior.
