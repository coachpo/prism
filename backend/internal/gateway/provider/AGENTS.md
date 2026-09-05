# Provider Adapters

`adapter.go` defines the shared adapter contract and `default_adapter.go` its safe defaults. Read [OpenAI guidance](openai/AGENTS.md) for native text/image behavior; Anthropic and Gemini implementations start at their respective `adapter.go` files.

- Unsupported operation shapes must return explicit errors or unsupported capability results. `DefaultAdapter` must not become a production passthrough fallback.
- Keep native request paths, response usage, and provider classifications here. Runtime owns operation matching, transport, stream orchestration, audit capture, and final telemetry; routing owns attempt order.
- Model-bound request DTOs name the selected Terminal Target wire identity `UpstreamModelID`. Never substitute requested/resolved Prism model identities.
- Update `adapter_conformance_test.go` when changing shared interfaces or declared hook behavior, plus the affected provider's native tests and runtime boundary coverage.
