# OpenAI Adapter

`adapter.go` owns native Chat/Responses requests and their adjunct paths; `response.go` owns non-stream usage and overflow classification; `images.go` owns image audit-body redaction for JSON and SSE.

- Keep Chat Completions and Responses native. Runtime checks text-mode equality and operation support before selecting attempts; this adapter must not convert sibling operations, reorder attempts, or provide best-effort fallback.
- Responses input-token counting and compaction require Responses-capable targets. Keep text and image capability dimensions independent.
- Native text model rewriting uses the selected Terminal Target's `UpstreamModelID`. Image operations use JSON-body model binding in runtime and do not need a separate adapter-level media request type.
- For streaming Chat Completions, inject `stream_options.include_usage = true` only when the caller has not declared `include_usage`; preserve every declared value, including false. Do not apply this injection to Responses, adjuncts, or images.
- Preserve native response usage. Streaming terminal/usage orchestration stays in runtime hooks; coordinate changes with `../../../httpapi/runtime/operation_response_hooks_test.go` and `operation_hook_residency_test.go`.
