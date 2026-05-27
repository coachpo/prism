# BACKEND MANAGEMENT AUDIT KNOWLEDGE BASE

## OVERVIEW
`management/audit/` owns selected-profile audit-log reads under `/api/audit/*` plus management job list/get/cancel routes under `/api/management/jobs*`. It serves audit-log browsing, audit-log detail lookup, and management-job status or cancellation without moving request execution or retention ownership into this package.

## STRUCTURE
```text
audit/
└── service.go    # Service construction, audit-log routes, management-job routes, filter parsing
```

## WHERE TO LOOK
- Route list and mount contract: `service.go`.
- Audit-log list/detail routes and supported filters: `service.go`, `../../../domain/audit/`.
- Request-log audit-capture availability checks: `service.go`, `request_logs` lookup helpers.
- Management job list/get/cancel flows: `service.go`, `../../../platform/managementjobs/`.

## CONVENTIONS
- Keep audit-log reads selected-profile scoped through effective-profile resolution.
- Keep audit list windows bounded and explicit; unsupported filters and ascending sort stay rejected.
- Keep management job status and cancellation here, while job creation and retention settings stay in `settings/` and platform workers.
- Keep audit-log payload reads separate from runtime request execution and request-log retention ownership.

## LLM UPSTREAM MATRIX
- When audit or observability behavior changes, evaluate request and response evidence across supported OpenAI, Anthropic, and Gemini operation shapes rather than assuming one provider family covers all audit rows.

## ANTI-PATTERNS
- Do not move audit-log reads into runtime handlers.
- Do not treat management job creation or retention settings as owned here.
- Do not allow unsupported audit filters, oversized windows, or ascending sort to slip through request parsing.
- Do not bypass request-time audit-capture checks when resolving audit-log lookups tied to request logs.
