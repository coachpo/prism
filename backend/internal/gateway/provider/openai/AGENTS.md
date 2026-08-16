# BACKEND OPENAI PROVIDER KNOWLEDGE BASE

## OVERVIEW
`gateway/provider/openai/` owns native OpenAI adapter behavior for Chat Completions, Responses, Responses input-token counting, Responses compaction, and image audit-body redaction. Runtime owns operation matching, native-compatibility planning, transport, stream parsing, auditing, and final telemetry.

## STRUCTURE
```text
openai/
├── adapter.go        # Native OpenAI operation paths and adapter contract
├── images.go         # Image request/response audit redaction (JSON and SSE)
├── response.go       # Native response usage and overflow handling
└── *_test.go         # Native adapter and boundary coverage
```

## WHERE TO LOOK
- Supported OpenAI text operations and native upstream paths: `adapter.go`
- Streaming usage instrumentation on the request side: `adapter.go` (`ensureChatCompletionsStreamUsage`)
- Image audit-body redaction for both the JSON and event-stream shapes: `images.go`
- Native response usage extraction and overflow classification: `response.go`
- Runtime native-compatibility planning and hook coverage: `../../../httpapi/runtime/operation_translation.go`, `../../../httpapi/runtime/operation_response_hooks_test.go`, `../../../httpapi/runtime/operation_hook_residency_test.go`

## CONVENTIONS
- Planning order and native operation-set compatibility are owned by runtime access-target resolution; this adapter must not reorder attempts or convert sibling OpenAI operations.
- Keep Responses adjunct operations native-only. `openai.responses.input_tokens` and `openai.responses.compact` require responses-capable targets.
- Preserve canonical usage from native upstream response bodies; stream usage and terminal events remain runtime-hook responsibilities.
- Image operations bind their model from the JSON body like the text operations, so they need no adapter-level media request type; only redaction lives here.
- Streaming Chat Completions bodies get `stream_options.include_usage = true` injected here so the upstream emits its final usage chunk; a caller-declared `include_usage` is never overridden, and the injection never applies to `openai.responses`, the Responses adjuncts, or the image operations.

## ANTI-PATTERNS
- Do not add runtime route matching or model graph traversal here.
- Do not silently pass unknown OpenAI APIs through the adapter.
- Do not reintroduce Chat Completions/Responses wire conversion or best-effort sibling fallback.
