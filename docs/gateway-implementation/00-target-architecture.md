# Gateway Target Architecture

## Purpose

This document defines the clean target architecture for Prism's LLM gateway/proxy. It follows the consolidated audit verdict: replace the current gateway core cleanly, preserve proven behavior and tests, and avoid compatibility shims for internal abstractions that have not shipped to users.

## Principles

- Endpoint handlers are thin facades over one shared gateway pipeline.
- Supported operations are explicit; `/v1` and `/v1beta` are not catch-all passthrough prefixes.
- Provider-specific protocol behavior belongs behind provider adapters.
- Model, upstream, route, pricing, and provider capability data are explicit registries.
- Streaming has a hard commit boundary: no retry, hedge, redirect, or overflow replay after the first downstream byte or event.
- Usage, pricing, logs, and audit events preserve requested model, effective model, selected upstream, route reason, usage source, and pricing catalog version.

## Target Module Layout

```text
backend/internal/httpapi/runtime/
  facade.go                 # HTTP method/path facade, request size limits, auth handoff
  response_writer.go        # HTTP response commit wrapper only

backend/internal/gateway/core/
  pipeline.go               # Shared phase runner
  envelope.go               # RequestEnvelope, ProviderRequest, RoutePlan, AccountingEvent
  errors.go                 # Gateway-domain errors
  phases.go                 # Typed phase interfaces and hook execution

backend/internal/gateway/registry/
  operations.go             # Method/path/shape operation registry
  models.go                 # ModelProfile registry
  upstreams.go              # UpstreamEndpoint registry
  routes.go                 # RouteRule, RoutePolicy, RoutePlan registry
  pricing.go                # PriceCatalog registry
  capabilities.go           # ProviderCapability registry

backend/internal/gateway/provider/
  adapter.go                # ProviderAdapter interface
  openai/                   # Chat, Responses, Images, token count, compact, conversion
  anthropic/                # Messages and token counting
  gemini/                   # generateContent, streamGenerateContent, countTokens

backend/internal/gateway/routing/
  planner.go                # Model redirect, upstream redirect, route reasons
  admission.go              # QPS/RPM/TPM/IPM/concurrency reservation
  overflow.go               # Context window estimation and backup model selection

backend/internal/gateway/streaming/
  pump.go                   # SSE/body streaming pump and commit boundary
  terminal.go               # Terminal event and usage classifiers

backend/internal/gateway/accounting/
  events.go                 # Attempt and final accounting events
  logging.go                # Request log persistence
  audit.go                  # Immutable audit event persistence
  pricing.go                # Pricing calculation and catalog version capture
```

## Key Interfaces

```go
type OperationRegistry interface {
    Resolve(method string, path string) (Operation, AllowedMethods, bool)
}

type ProviderAdapter interface {
    ParseRequest(Operation, RequestEnvelope) (ProviderRequest, error)
    BuildUpstreamRequest(ProviderRequest, RouteAttempt) (UpstreamRequest, error)
    AdaptNonStreamResponse(Operation, UpstreamResponse) (ClientResponse, UsageEnvelope, error)
    StreamPump(Operation, StreamRequest) (StreamResult, error)
    EstimateTokens(Operation, ProviderRequest) (TokenEstimate, error)
    ClassifyOverflow(Operation, UpstreamResponse) OverflowClassification
}

type RoutePlanner interface {
    Plan(RequestEnvelope, Operation, ProviderRequest) (RoutePlan, error)
}
```
```go
type AdmissionController interface {
    Reserve(RouteAttempt, UsageEstimate) (AdmissionLease, AdmissionDecision)
}

type HookPhase[T any] interface {
    Run(context.Context, T) (T, error)
}

type AccountingSink interface {
    RecordAttempt(context.Context, AccountingEvent) error
    RecordFinal(context.Context, AccountingEvent) error
}
```

Core configuration records:

- `ModelProfile`: public model id, provider family, context window, redirect targets, overflow backup model, response-shape preservation policy.
- `UpstreamEndpoint`: endpoint id, provider family, base URL, auth profile, supported native operations, pricing catalog binding, quota limits.
- `RouteRule` / `RoutePolicy`: matching rule, upstream narrowing, model redirect, failover policy, load-balance strategy, admission policy.
- `RoutePlan`: resolved operation, requested model, effective model, candidate upstreams, route reason, selected attempt order.
- `PriceCatalog`: pricing unit, input/output/cache/reasoning prices, currency, catalog version.
- `ProviderCapability`: native operations, streaming support, token estimate/count support, overflow classifier, conversion support, image support.

## Data Flow Diagram

```text
client request
  -> endpoint facade
  -> OperationRegistry.Resolve
  -> RequestEnvelope
  -> ProviderAdapter.ParseRequest
  -> RoutePlanner.Plan
  -> AdmissionController.Reserve
  -> ProviderAdapter.BuildUpstreamRequest
  -> upstream provider
  -> ProviderAdapter response/stream adaptation
  -> AccountingSink attempt/final events
  -> client response
```

## Endpoint Flow Diagram

```text
/v1/chat/completions            -> OpenAI adapter operation: chat_completions
/v1/responses                   -> OpenAI adapter operation: responses
/v1/responses/input_tokens      -> OpenAI adapter operation: responses_input_tokens
/v1/responses/compact           -> OpenAI adapter operation: responses_compact
/v1/images/generations          -> OpenAI adapter operation: image_generation
/v1/images/edits                -> OpenAI adapter operation: image_edit
/v1/messages                    -> Anthropic adapter operation: messages
/v1/messages/count_tokens       -> Anthropic adapter operation: count_tokens
/v1beta/models/{model}:generateContent       -> Gemini adapter operation: generate_content
/v1beta/models/{model}:streamGenerateContent -> Gemini adapter operation: stream_generate_content
/v1beta/models/{model}:countTokens           -> Gemini adapter operation: count_tokens
```

## Routing Flow Diagram

```text
requested model
  -> exact ModelProfile lookup
  -> optional model redirect
      -> re-enter normal route planning with redirected model
  -> RouteRule matching
  -> optional upstream redirect
      -> pin or narrow candidate UpstreamEndpoints
  -> candidate ordering by RoutePolicy
  -> admission reservation per candidate
  -> selected RouteAttempt
```

## Streaming Flow Diagram

```text
upstream stream opened
  -> pre-commit phase: failover allowed until first downstream byte/event
  -> first downstream byte/event committed
  -> post-commit phase: no retry, no hedge, no overflow replay, no upstream switch
  -> terminal classifier records completed/incomplete/client_disconnect/read_error
  -> usage merger extracts final usage when available
  -> accounting sink records immutable stream outcome
```

## Sequence Diagram: Context Overflow

```text
client -> facade: non-stream OpenAI-family request
facade -> pipeline: RequestEnvelope
pipeline -> adapter: parse + estimate/count tokens
pipeline -> routing: compare estimate with context windows
routing -> routing: choose backup ModelProfile when primary cannot fit
routing -> routing: backup model re-enters normal load balancing
routing -> admission: reserve selected upstream
admission -> provider: execute selected upstream
provider -> adapter: preserve public response shape when configured
adapter -> accounting: context_overflow decision + route reason + usage source
adapter -> client: final response
```

## Sequence Diagram: QPS Overflow

```text
pipeline -> routing: ordered candidate upstreams
routing -> admission: try candidate A
admission -> routing: reject, QPS exhausted
routing -> admission: try candidate B
admission -> routing: accepted lease
routing -> provider: execute candidate B
provider -> accounting: selected upstream B + route reason qps_overflow
provider -> client: response
```

## Sequence Diagram: Model Redirect

```text
pipeline -> routing: requested model public-alpha
routing -> registry: ModelProfile public-alpha has model redirect public-beta
routing -> routing: restart route planning for public-beta
routing -> admission: reserve normal candidate for public-beta
provider -> accounting: requested_model=public-alpha, effective_model=public-beta
provider -> client: response shape for requested operation
```

## Sequence Diagram: Upstream Redirect

```text
pipeline -> routing: requested model public-alpha
routing -> registry: RouteRule matches request attributes
routing -> routing: upstream redirect pins endpoint group openai-east
routing -> routing: candidate set narrowed to openai-east endpoints
routing -> admission: reserve first available narrowed candidate
provider -> accounting: selected_upstream + route_reason upstream_redirect
provider -> client: response
```

## Logging/Audit/Usage/Pricing Flow

```text
attempt starts
  -> AccountingEvent(attempt): request id, operation, requested model, effective model, candidate upstream, route reason
attempt finishes
  -> usage source extracted by ProviderAdapter
  -> PriceCatalog version resolved from selected upstream/model policy
  -> pricing calculated from normalized usage
  -> request log row records mutable operational detail
  -> audit log row records immutable audit event and optional redacted bodies
  -> usage event records token and cost facts
  -> telemetry/outbox emits post-commit side effects
```

## Non-Goals

- Do not preserve the current `requestPlan` shape as a compatibility contract.
- Do not support unknown vendor paths through generic passthrough.
- Do not retry, hedge, or overflow-promote after a streaming response is committed.
- Do not infer pricing, route reason, or usage source after the fact when the phase that knows it can emit typed data.
