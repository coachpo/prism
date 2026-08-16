# BACKEND INTERNAL KNOWLEDGE BASE

## OVERVIEW
`backend/internal/` owns Prism's live Go implementation below the process entrypoint. It routes work between platform infrastructure, HTTP surfaces, gateway/runtime contracts, HTTP-neutral domains, PostgreSQL helpers, and small shared packages.

## STRUCTURE
```text
internal/
├── platform/      # lifecycle, config, DB lanes, HTTP assembly, workers, retention
├── httpapi/       # management API, runtime proxy, request context
├── gateway/       # provider-neutral envelopes, hooks, adapters, routing, accounting
├── domain/        # HTTP-neutral audit, loadbalance, model-routing, stats, safediag, terminal targets
├── pgxutil/       # shared PostgreSQL transaction helpers
├── endpointdomain/
├── openaimodecheck/ # read-only OpenAI text mode equality check (preflight + startup fail-fast)
├── profiledomain/
└── providerauth/
```

## WHERE TO LOOK
- Production composition, bootstrap config, DB lanes, scheduler, side effects, and retention: `platform/AGENTS.md`
- Safe failure-diagnostic bottom line (scrub/extract/codes/metadata/limits), shared by runtime diagnostics, v1 drain, and backfill: `domain/safediag/AGENTS.md`
- HTTP mount seams, management fanout, runtime proxy ingress, request-context helpers, and proxy-key usage: `httpapi/AGENTS.md`
- Runtime gateway contracts, provider adapters, route planning, reservations, hook execution, and accounting records: `gateway/AGENTS.md`
- Audit, loadbalance, stats, terminal-target, and model-routing helpers that must stay HTTP-neutral: `domain/AGENTS.md`
- SQL transaction helper ownership: `pgxutil/tx.go`
- Small shared packages used across management/runtime boundaries: `endpointdomain/`, `profiledomain/`, `providerauth/`
- OpenAI text mode equality check shared by the upgrade preflight and the startup fail-fast step: `openaimodecheck/`

## CONVENTIONS
- Keep this file as the router. Put package-specific facts in the nearest child `AGENTS.md`.
- Keep platform process concerns out of `domain/` and gateway provider concerns out of `httpapi/`.
- Keep runtime operation support anchored in `httpapi/runtime/operations.go`; shared packages do not widen supported routes.
- Keep PostgreSQL helper behavior in `pgxutil/` boring and shared. Do not hide feature policy there.
- Small shared packages stay narrow translation surfaces. If they grow policy, move that policy to the owning domain or HTTP API package.
- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS
- Do not invent a second runtime surface outside `httpapi/runtime/` and `gateway/`.
- Do not put HTTP request parsing, auth headers, Default-profile management scope, or response shaping into `domain/`.
- Do not put provider-specific routing branches in `platform/`, `domain/`, or shared compatibility helpers.
- Do not bypass `platform/logretention/`, runtime partition ensuring, durable outboxes, or background workers from sibling packages.
