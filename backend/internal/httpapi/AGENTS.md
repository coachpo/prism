# HTTP API Guidance

Keep handler ownership separate from server assembly: management handlers live in [management](management/AGENTS.md), runtime execution in [runtime](runtime/AGENTS.md), and mounting, admission, browser-write guards, and mutation invalidation in `../platform/http/`.

- Reuse `requestcontext/` for ingress identity, management principal, and runtime proxy-key attribution. Reuse `proxykeyusage/` for the shared usage record instead of creating endpoint-local equivalents.
- Keep management session/cookie/token helpers in `management/auth/`; runtime proxy authentication uses that package's distinct runtime middleware and published cache.
- Keep HTTP parsing and response shaping here; shared audit, statistics, routing, and value semantics belong in `../domain/`.
- Retained statistics and audit reads are management APIs. Do not add a second runtime observability or bootstrap-configuration CRUD surface.

For implementation checks, use the affected package tests under `internal/httpapi/` and the matching [regression suite](../../tests/AGENTS.md); the backend validation commands remain in [CONTRIBUTING.md](../../../CONTRIBUTING.md).
