# BACKEND RUNTIME HTTPAPI KNOWLEDGE BASE

## OVERVIEW
`runtime/` owns Prism's runtime proxy surface under `/v1/*`, `/v1/messages*`, and `/v1beta/models/*`: request planning, upstream auth/header shaping, runtime cache reads, request-generation params, telemetry outbox enqueue, feedback-pipeline handoff, runtime side effects, and log-partition ensuring.

## STRUCTURE
```text
runtime/
├── runtime.go                  # Route entry, request planning, upstream proxy flow
├── service.go                  # Service wiring, pools, HTTP client, workers
├── cache.go                    # Shared runtime cache reads and snapshots
├── request_generation_params.go# Request-generation param parsing and normalization
├── generations.go              # Generation-request shaping and streaming helpers
├── log_partitions.go           # Runtime partition ensuring and cache
├── observability.go            # Request-log and usage event shaping
├── telemetry_outbox.go         # Durable telemetry enqueue and publisher wakeups
├── feedback_pipeline.go        # Runtime feedback persistence and worker handoff
├── runtime_side_effects.go     # Runtime side-effect manager and shutdown behavior
└── runtime_pricing.go          # Runtime pricing snapshots and usage pricing helpers
```

## WHERE TO LOOK
- Route entry, provider path matching, request replay rules, and upstream auth/header control: `runtime.go`, `generations.go`
- Service construction, pool ownership, runtime transport config, and worker registration: `service.go`
- Runtime cache, planning snapshots, proxy selector helpers, and request-generation params: `cache.go`, `planning_snapshot.go`, `proxy_selector_helpers.go`, `request_generation_params.go`
- Telemetry, feedback, and side-effect ownership: `telemetry_outbox.go`, `feedback_pipeline.go`, `runtime_side_effects.go`
- Partition ensuring and partition-cache behavior: `log_partitions.go`, `../../../platform/logretention/`
- Runtime regression coverage: `*_test.go`, `../../../tests/runtime/`

## CONVENTIONS
- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep runtime proxy planning here; selected-profile management scope does not belong on proxy traffic.
- Keep telemetry, feedback, and side-effect work on the durable outbox or worker seams instead of the hot request path.
- Keep runtime partition ensuring here plus `platform/logretention/`; handlers must not create or drop partitions ad hoc.

## ANTI-PATTERNS
- Do not inject management-only `X-Profile-Id` logic or auth-session state into runtime proxy handlers.
- Do not bypass the telemetry outbox, feedback pipeline, or runtime side-effect manager with inline writes or sends.
- Do not duplicate upstream auth/header shaping or provider-path parsing in sibling packages.
- Do not run retention cleanup or partition maintenance outside `log_partitions.go` and `platform/logretention/`.
