# BACKEND GATEWAY KNOWLEDGE BASE

## OVERVIEW
`backend/internal/gateway/` owns the provider-agnostic runtime gateway contracts below `../httpapi/runtime/`: core request/response envelopes, hook phases, provider adapters, route planning, reservation limits, and accounting event normalization.

## STRUCTURE
```text
gateway/
├── accounting/     # Runtime accounting event normalization
├── core/           # Pipeline interfaces, envelopes, hook executor, route/accounting types
│   └── AGENTS.md   # Shared gateway core contracts
├── provider/       # Provider adapter contract plus OpenAI, Anthropic, Gemini adapters
│   └── AGENTS.md   # Provider-native adapter and capability rules
└── routing/        # Candidate ordering, reservation checks, retry/redirect planning
```

## WHERE TO LOOK
- Runtime ingress rejection, operation allowlist, and streaming safety: `../httpapi/runtime/operations.go`, `../httpapi/runtime/service.go`, `../httpapi/runtime/service_ingress_test.go`
- Pipeline seams and shared envelopes: `core/AGENTS.md`, `core/pipeline.go`, `core/envelope.go`, `core/routing.go`, `core/errors.go`
- Hook phase ordering, permissions, payload cloning, rejection behavior, and execution records: `core/hooks.go`, `core/hooks_test.go`
- Provider adapter interface, default behavior, token/conversion contracts, and hook-behavior declarations: `provider/AGENTS.md`, `provider/adapter.go`, `provider/default_adapter.go`
- Native OpenAI Chat/Responses paths, usage parsing, and overflow classification: `provider/openai/`
- Anthropic Messages/count-token path rewriting, usage extraction, and stream terminal classification: `provider/anthropic/`
- Gemini model-path rewriting, GenerateContent variants, token counting, and stream parsing: `provider/gemini/`
- Candidate ordering, reservation admission, retry-window policy, redirect narrowing, and route-reason canonicalization: `routing/planner.go`, `routing/reservation_manager.go`, `routing/retry_policy.go`, `routing/redirects.go`
- Runtime integration and request-log/accounting use: `../httpapi/runtime/`, especially `service.go`, `ingress.go`, `request_execution.go`, `upstream_attempt.go`, `telemetry_records.go`, and `observability.go`

## CONVENTIONS
- Any UI/UX-facing guidance or frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change must defer to `frontend/DESIGN.md`; keep backend docs focused on the Go runtime contract instead of repeating design-system rules.
- Keep `httpapi/runtime/operations.go` as Prism's concrete runtime allowlist; gateway routing code stays generic and reusable.
- Keep provider-native request/response/stream behavior inside provider adapters, not in route planning or accounting.
- Keep hook payloads clone-safe and permission-gated. Do not leak body/header access beyond declared hook permissions.
- Keep route reasons canonical across `core`, `routing`, runtime observability, and accounting.
- Keep reservation decisions in `routing/` and release owned reservations when runtime attempts end.
- When gateway work touches upstream request or response logic, evaluate streaming and non-streaming coverage for OpenAI Chat/Responses, Anthropic, and Gemini.

## ANTI-PATTERNS
- Do not add provider-specific branching to `routing/` or `core/`.
- Do not duplicate runtime operation definitions here; bridge from the runtime allowlist.
- Do not treat gateway adapters as a generic passthrough for unsupported vendor APIs.
- Do not bypass accounting normalization when adding new route reasons or usage sources.
