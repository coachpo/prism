# Context Overflow Promotion Regression Matrix

## Purpose

This supporting reference tracks CLIProxyAPI context-overflow regressions across OpenAI Chat Completions and Responses surfaces. It is not the source of truth for runtime behavior; keep `API_SPEC.md`, `ARCHITECTURE.md`, `DATA_MODEL.md`, and backend runtime code authoritative.

Use this file when adding or reviewing automated regressions under `backend/internal/httpapi/runtime`, `backend/internal/httpapi/management`, `backend/tests/runtime`, and the frontend config validation suites.

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
- Ordinary routing stays flat where compatible; recursive context-overflow planning is a separate pre-dispatch path.
- Recursive pre-dispatch planning can walk explicit chains for at most `3` promotion transitions.
- Provider-overflow fallback is one-shot and non-recursive.
- No promotion target is inferred from window size, pricing, labels, endpoints, facade siblings, or operation shape.
- No provider-overflow replay can occur after client-visible bytes.
- Requested model identity remains the client-supplied model; final model and terminal metadata are additive in request logs, usage events, and traces.

## Runtime Trigger Matrix

| ID | Trigger path | Ingress operation | Streaming | Chain shape | Expected behavior |
|---|---|---|---:|---|---|
| COP-R01 | recursive pre-dispatch | `/v1/chat/completions` | no | Chat to Chat to Chat | skips non-fitting source and middle, dispatches final fitting terminal |
| COP-R02 | recursive pre-dispatch | `/v1/chat/completions` | yes | Chat to routing model | source upstream is not opened, final child terminal receives Chat stream |
| COP-R03 | recursive pre-dispatch | `/v1/responses` | no | Responses to Responses | first fitting frame wins before provider transport |
| COP-R04 | recursive pre-dispatch | `/v1/responses` | yes | Responses to Chat-capable terminal | translation must preserve Responses stream shape when adapter-approved |
| COP-F01 | provider-overflow fallback | `/v1/chat/completions` | no | source to explicit target | source overflow can replay once, target failure or overflow is final |
| COP-F02 | provider-overflow fallback | `/v1/responses` | yes | source to explicit target | replay only before visible bytes, staged SSE prelude remains bounded |
| COP-F03 | provider-overflow fallback | `/v1/chat/completions` or `/v1/responses` | any | pre-dispatch already promoted | selected final model cannot chain again after provider overflow |

## Recursive Pre-Dispatch Regressions

| Area | Regression | Coverage note |
|---|---|---|
| Planner success | `TestBuildRequestPlanRecursiveContextOverflow` | Selects the first fitting terminal across an explicit promotion chain without provider calls to non-fitting frames. |
| Normal route wins | `TestBuildRequestPlanRecursiveContextOverflowNormalFitShortCircuits` | Keeps ordinary routing flat when the current model already has a fitting terminal target. |
| Stop reasons | `TestBuildRequestPlanRecursiveContextOverflowStopsOnMissingEstimation` | Records `estimation_unavailable` and leaves safe provider-overflow fallback eligible. |
| Stop reasons | `TestBuildRequestPlanRecursiveContextOverflowStopsOnMissingWindow` | Records `missing_context_window` instead of treating missing capacity as fitting or zero. |
| Chain guardrails | `TestBuildRequestPlanRecursiveContextOverflowRejectsCycle` | Rejects explicit cycles before provider transport. |
| Chain guardrails | `TestBuildRequestPlanRecursiveContextOverflowRejectsMaxDepth` | Enforces the runtime-owned `3` transition limit. |
| Routing model target | `TestBuildRequestPlanRecursiveContextOverflowPromotionTargetRoutingModel` | Allows a promotion target to resolve through its own normal routing model plan. |
| Flat routing preserved | `TestResolveModelAccessFromRoutingPlanFlatPoolPreservedForOrdinaryModels` | Keeps ordinary direct/model-target pools source ordered and strategy scoped. |
| Facade guardrail | `TestResolveExactFacadeModelRejectsDirectTerminalTargetsDuringRecursivePlanning` | Rejects direct Terminal Targets on facade models during recursive promotion target evaluation only. |
| End-to-end final terminal | `TestContextOverflowPromotionRecursivePreDispatchChoosesFinalTerminal` | Proves source and intermediate models receive zero upstream requests and the final terminal is selected before dispatch. |

## Provider-Overflow Fallback Regressions

| Area | Regression | Coverage note |
|---|---|---|
| One-shot fallback | `TestContextOverflowPromotionProviderFallbackOneShot` | Source provider overflow may replay once to the explicit target. |
| No multi-hop fallback | `TestPromotionDoesNotMultiHop` | A promoted provider-overflow result stays final. |
| Recursive success is final | `TestContextOverflowPromotionPreDispatchSelectionDoesNotProviderFallbackAgain` | A final model selected by recursive pre-dispatch planning cannot start another provider-overflow promotion. |
| Unavailable estimation fallback | `TestResponsesStreamingProviderOverflowStillReplaysBeforeVisibleBytesWhenEstimateUnavailable` | Halted recursive metadata does not block pre-visible provider-overflow replay. |
| Visible bytes stop replay | `TestResponsesStreamingSSEOutputDeltaThenOverflowDoesNotReplay` | Semantic SSE output commits the source stream and disables replay. |
| Existing metadata | `TestProviderOverflowPromotionRequestLogAndUsageMetadata/non_stream_chat` | Non-stream Chat provider overflow records additive request-log and usage metadata. |
| Existing metadata | `TestProviderOverflowPromotionRequestLogAndUsageMetadata/responses_streaming` | Streaming Responses replay hides source overflow bytes and records provider-overflow metadata. |

## Validation Regressions

| Area | Regression | Coverage note |
|---|---|---|
| CRUD valid chain | `TestModelServiceAcceptsRecursivePromotionChainWithoutImmediateLargerWindow` | Accepts explicit `A -> B -> C` chains without requiring the immediate target to fit by window. |
| CRUD cycle | `TestModelServiceRejectsPromotionCycle` | Rejects cycles on `context_overflow_promotion_target_id`. |
| CRUD depth | `TestModelServiceRejectsPromotionMaxDepth` | Rejects chains beyond `3` transitions. |
| CRUD terminal loop | `TestModelServiceRejectsSameTerminalPromotionTarget` | Rejects promotion targets that resolve back to the source terminal set. |
| CRUD scope and family | `TestModelServiceRejectsCrossProfilePromotionTarget`, `TestModelServiceRejectsAPIFamilyMismatchPromotionTarget` | Keeps exact ID, same profile, same family validation. |
| Config bundle valid chain | `TestBundlePreviewAcceptsRecursivePromotionChainWithoutImmediateLargerWindow` | Preview and execute accept valid explicit recursive chains with the v3 payload shape unchanged. |
| Config bundle guardrails | `TestBundleImportRejectsPromotionCycle`, `TestBundleImportRejectsPromotionMaxDepth`, `TestBundleImportRejectsSameTerminalPromotionTarget`, `TestBundleImportRejectsMissingPromotionTarget` | Mirrors CRUD errors for import and preview. |
| Export parity | `TestBundleExportIncludesPromotionTarget` | Keeps `context_overflow_promotion_target_id` in exported model payloads. |
| Frontend authoring | model dialog payload and E2E promotion cycle tests | Keeps promotion target separate from access targets and renders backend `routing_plan_issues`. |
| Frontend import validation | `frontend/tests/lib/config_import_validation_contract.test.mjs` recursive promotion cases | Mirrors explicit-chain depth, cycle, facade, disabled, family, self, missing-target, and same-terminal checks where bundle data allows it. |

## Observability Regressions

| Area | Regression | Coverage note |
|---|---|---|
| Recursive metadata | `TestPreDispatchPromotionMetadataIncludesRecursiveChain` | Adds `promotion_chain`, `promotion_depth`, final model, final terminal, and trigger phase additively. |
| Trace metadata | `TestRuntimeTraceContextOverflowPromotionRecursiveChain` | Emits recursive trace attributes with model IDs redacted. |
| Request-log persistence | recursive request-log contract tests under `backend/tests/runtime` | Persists context-routing metadata without prompt text or raw request content. |
| Halted metadata | Task 6 provider-overflow regression tests | Confirms `result=not_promoted` metadata doesn't block provider-overflow fallback after unavailable estimation. |

## Source References

- Runtime planner tests: `backend/internal/httpapi/runtime/runtime_test.go`
- Runtime promotion metadata tests: `backend/internal/httpapi/runtime/context_overflow_promotion_metadata_test.go`
- Runtime end-to-end tests: `backend/tests/runtime/context_overflow_promotion_test.go`, `backend/tests/runtime/proxy_selector_test.go`, `backend/tests/runtime/request_logs_contract_test.go`
- Runtime translation tests: `backend/internal/httpapi/runtime/operation_translation_request_test.go`, `backend/internal/httpapi/runtime/operation_translation_stream_test.go`
- Runtime promotion gates: `backend/internal/httpapi/runtime/service.go`
- Runtime recursive planning: `backend/internal/httpapi/runtime/runtime_planner.go`, `backend/internal/httpapi/runtime/planning_snapshot.go`, `backend/internal/httpapi/runtime/runtime.go`
- Management target validation: `backend/internal/httpapi/management/models/routes.go`, `backend/internal/httpapi/management/models/promotion_target_test.go`
- Config-bundle target validation: `backend/internal/httpapi/management/configbundle/import.go`, `backend/internal/httpapi/management/configbundle/promotion_target_test.go`
- Frontend import validation: `frontend/src/lib/configImportValidation.ts`, `frontend/tests/lib/config_import_validation_contract.test.mjs`

## Maintenance Notes

When adding recursive pre-dispatch regressions, assert both planning and observability:

- normal-path fit short-circuits before promotion target evaluation
- non-fitting frames open zero upstream requests
- promotion chains use explicit model IDs only and stop at `3` transitions
- selected terminal target, final selected model, `promotion_chain`, `promotion_depth`, `trigger_phase`, `estimation_mode`, and stop reasons are additive
- requested model identity remains the client-supplied model

When adding provider-overflow fallback regressions, assert the fallback distinction directly:

- provider-overflow replay starts only from classified provider overflow
- replay happens at most once and never creates a ladder
- recursive pre-dispatch success disables later provider-overflow chaining
- streaming Responses replay happens only before visible bytes
- overflow affinity and preselection remain provider-overflow-only

Do not add tests for non-stream-to-stream or stream-to-non-stream promotion. Those are invalid scenarios for this feature. Do not describe `/v1` or `/v1beta` as broad passthrough prefixes; supported operations stay operation-registered.
