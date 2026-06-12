# Context Overflow Promotion Regression Matrix

## Purpose

This supporting reference preserves the runtime regression matrix for CLIProxyAPI context overflow promotion across OpenAI Chat Completions and Responses surfaces. It is not the source of truth for runtime behavior; keep `API_SPEC.md`, `ARCHITECTURE.md`, `DATA_MODEL.md`, and the backend runtime code authoritative.

Use this file when adding or reviewing future automated regressions under `backend/tests/runtime/context_overflow_promotion_test.go` and adjacent runtime translation tests.

## Scope

The matrix covers only OpenAI text operations that can participate in context overflow promotion:

- `POST /v1/chat/completions`
- `POST /v1/responses`

Promotion must keep the client request streaming mode unchanged. A non-stream request must not become a streaming upstream request, and a streaming request must not become a non-stream upstream request.

Cross-format promotion is valid only inside the OpenAI `api_family` text bridge:

- Chat ingress can promote to a Responses-only target when translation is supported.
- Responses ingress can promote to a Chat-only target when translation is supported.

Different Prism `api_family` values are not runtime matrix cases. A promotion target with a different `api_family` must be rejected by model/config-bundle validation before runtime.

## Invariants

- Promotion target selection is explicit and model-scoped through `context_overflow_promotion_target_id`.
- Source and promotion target must share the same Prism `api_family`.
- OpenAI text format can differ inside the same `api_family` when request, response, and stream translation are supported.
- Streaming mode is preserved end to end.
- Promotion is one-shot. A promoted target overflow must not trigger another promotion.
- Requested model identity remains the client-supplied model; promotion metadata is additive in request logs, usage events, and traces.

## Positive Runtime Matrix

| ID | Ingress operation | Streaming | Promotion target format | Expected promoted upstream | Expected trigger |
|---|---|---:|---|---|---|
| COP-P01 | `/v1/chat/completions` | no | Chat | `/v1/chat/completions` | provider overflow |
| COP-P02 | `/v1/chat/completions` | yes | Chat | `/v1/chat/completions` | pre-dispatch estimate |
| COP-P03 | `/v1/chat/completions` | no | Responses | `/v1/responses` | provider overflow |
| COP-P04 | `/v1/chat/completions` | yes | Responses | `/v1/responses` | pre-dispatch estimate |
| COP-P05 | `/v1/responses` | no | Responses | `/v1/responses` | provider overflow |
| COP-P06 | `/v1/responses` | yes | Responses | `/v1/responses` | provider overflow in pre-visible/pre-commit streaming path |
| COP-P07 | `/v1/responses` | no | Chat | `/v1/chat/completions` | provider overflow |
| COP-P08 | `/v1/responses` | yes | Chat | `/v1/chat/completions` | provider overflow in pre-visible/pre-commit streaming path |

## Already Covered

| Matrix IDs | Existing regression | Coverage note |
|---|---|---|
| COP-P01 | `TestProviderOverflowPromotionRequestLogAndUsageMetadata/non_stream_chat` | Non-stream Chat provider overflow promotes to a same-format target and records request-log/usage metadata. |
| COP-P02 | `TestChatStreamingPreDispatchPromotionSkipsSourceUpstream` | Streaming Chat promotes before source dispatch; source upstream receives no request. |
| COP-P02 | `TestChatStreamingPreDispatchPromotionRequestLogAndTraceMetadata` | Streaming Chat promotion records `pre_dispatch_estimate`, token estimates, and attempt counts. |
| COP-P05 | `TestResponsesOverflowPromotesToDualNativeTargetWithoutTranslation` | Non-stream Responses promotes to a native/dual-native Responses target without translation. |
| COP-P06 | `TestProviderOverflowPromotionRequestLogAndUsageMetadata/responses_streaming` | Streaming Responses promotes through the pre-visible/pre-commit provider-overflow path and hides source overflow bytes. |
| COP-P07 | `TestAdapterGatedResponsesOverflowPromotesToChatOnlyTarget` | Non-stream Responses promotes to a Chat-only target; upstream uses `/v1/chat/completions` while the client receives a Responses-shaped result. |

## Missing Positive Regressions

| Priority | Matrix ID | Suggested test | Required assertions |
|---|---|---|---|
| high | COP-P03 | `TestChatOverflowPromotesToResponsesOnlyTarget` | Source receives `/v1/chat/completions`; promoted upstream receives `/v1/responses`; client receives Chat-shaped response; promotion metadata records `provider_overflow`. |
| high | COP-P04 | `TestChatStreamingPreDispatchPromotionToResponsesOnlyTarget` | Source upstream is not called; promoted upstream receives `/v1/responses`; client receives Chat SSE translated from a Responses stream; promotion metadata records `pre_dispatch_estimate`. |
| high | COP-P08 | `TestResponsesStreamingProviderOverflowPromotesToChatOnlyTarget` | Source emits only pre-visible promotable overflow; promoted upstream receives `/v1/chat/completions`; client receives Responses SSE translated from Chat stream; source overflow bytes do not leak. |

## Negative And Boundary Regressions

| Priority | Case | Suggested test | Required assertions |
|---|---|---|---|
| medium | Chat -> Responses unsupported non-stream request shape | `TestChatOverflowDoesNotPromoteToResponsesOnlyTargetForUnsupportedShape` | Unsupported Chat translation fields prevent promotion; promoted upstream is not called; original source overflow remains final. |
| medium | Chat -> Responses unsupported stream shape | `TestChatStreamingPreDispatchSkipsResponsesOnlyPromotionForUnsupportedStreamShape` | Tool or other unsupported stream shape prevents translated promotion; target is not called. |
| medium | Responses -> Chat unsupported stream shape | `TestResponsesStreamingDoesNotPromoteToChatOnlyTargetForUnsupportedStreamShape` | Unsupported Responses stream shape prevents translated promotion; target is not called or original overflow remains final. |
| medium | Different Prism `api_family` target | `TestPromotionTargetRejectsDifferentAPIFamilyRuntimeRelevantFamilies` | Management/config validation rejects before runtime with the `context_overflow_promotion_target_id` API-family mismatch error. |

## Source References

- Runtime positive tests: `backend/tests/runtime/context_overflow_promotion_test.go`
- Runtime translation tests: `backend/internal/httpapi/runtime/operation_translation_request_test.go`, `backend/internal/httpapi/runtime/operation_translation_stream_test.go`
- Runtime promotion gates: `backend/internal/httpapi/runtime/service.go`
- Runtime explicit-target planning: `backend/internal/httpapi/runtime/runtime.go`
- Runtime overflow classifier: `backend/internal/httpapi/runtime/operation_response_hooks.go`
- Management target validation: `backend/internal/httpapi/management/models/routes.go`
- Config-bundle target validation: `backend/internal/httpapi/management/configbundle/import.go`

## Maintenance Notes

When adding any missing positive regression, assert both transport behavior and observability:

- upstream path and target model received by source/promoted upstreams
- client-visible response or SSE format
- source overflow byte leakage for streaming Responses cases
- `context_overflow_promotion` metadata, trigger phase, source/final attempt counts, and requested-model identity

Do not add tests for non-stream-to-stream or stream-to-non-stream promotion. Those are invalid scenarios for this feature.
