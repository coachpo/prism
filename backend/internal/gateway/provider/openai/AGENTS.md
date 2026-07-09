# BACKEND OPENAI PROVIDER KNOWLEDGE BASE

## OVERVIEW
`gateway/provider/openai/` owns OpenAI-native adapter behavior for Chat Completions, Responses, Responses input-token counting, Responses compaction, and Chat/Responses sibling translation. Runtime still owns operation matching, planning order, transport, auditing, and final telemetry.

## STRUCTURE
```text
openai/
├── adapter.go                  # Adapter entrypoint and native text operation paths
├── translation_request.go      # Chat <-> Responses request conversion
├── translation_response.go     # Non-stream response conversion and usage capture
├── translation_stream.go       # SSE conversion between Chat and Responses streams
├── translation_stream_state.go # Stateful stream conversion accumulator
├── translation_content.go      # Content-part conversion helpers
├── translation_tools.go        # Tool/function-call conversion helpers
├── translation_reasoning.go    # Reasoning field normalization
├── translation_errors.go       # Translation-specific adapter errors
└── *_test.go                   # Adapter-level translation coverage
```

## WHERE TO LOOK
- Supported OpenAI text operations and native upstream paths: `adapter.go`
- Request translation, model rewriting, stream include-usage injection, and explicit translation loss: `translation_request.go`
- Response translation, canonical usage extraction, and unsafe header handling: `translation_response.go`
- Stream conversion state, terminal event handling, and Chat `[DONE]` reconstruction: `translation_stream.go`, `translation_stream_state.go`
- Tool/function-call and content-part support: `translation_tools.go`, `translation_content.go`
- Runtime-facing golden coverage: `../../../httpapi/runtime/operation_translation_golden_test.go`, `../../../httpapi/runtime/testdata/openai_translation/`

## CONVENTIONS
- Translation eligibility is adapter-approved per request shape. Do not describe it as generic OpenAI compatibility or a narrow passthrough.
- Planning order is owned by runtime access-target order; this adapter should report capability and conversion results, not reorder attempts.
- Keep Responses adjunct operations native-only. `openai.responses.input_tokens` and `openai.responses.compact` require responses-capable targets and do not translate to Chat Completions.
- Preserve canonical usage from upstream response bodies or terminal stream events; translated client shapes should not invent usage.
- Keep unsupported metadata visible through explicit `TranslationLoss` or deterministic adapter errors.

## ANTI-PATTERNS
- Do not add runtime route matching or model graph traversal here.
- Do not silently pass unknown OpenAI APIs through the adapter.
- Do not hide lossy translation behind successful conversion without recording the loss.
