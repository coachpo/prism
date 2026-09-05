# Auth Guidance

Keep management session enforcement in `management_auth_middleware.go` and runtime proxy-key enforcement/attribution in `runtime_middleware.go`. Use `runtime_cache.go` for published runtime decisions rather than querying management state from runtime handlers.

- `problems.go` owns typed auth errors. Keep the frontend known-code decoder, recovery classifier, and locale entries aligned with route, details, retry, and recovery contracts.
- Public auth status is the tagged `enabled|disabled|transition_fail_closed` union. Authenticated session responses carry the server-authored `subject_key`; public and anonymous responses do not.
- `enabling_fail_closed` and `rollback_required` block ordinary management while auth control, status, and operation recovery remain reachable. `disabling_enforced` retains the old enabled gate.
- Preserve the transaction in `auth_settings_transaction.go`: affected Requests/Audit writer admission, Proxy readiness fence, replay check, immutable staging/session changes, operation recording, and final pointer publication. Transaction commit is the linearization point; cache and cookie publication occurs afterward in `auth_settings_publish.go`.
- Readiness comes from `proxy_key_readiness_fence.go`, using one server-clock count and the 30-second safe-active horizon. Key mutations acquire the same fence before the auth-control singleton; do not invent another activation predicate.
- Proxy-key rotation updates the same row so retained attribution keeps a stable logical key id. Preserve serialized capacity, presence-aware expiry (omitted preserves, null clears), and private/no-store one-time create/rotate responses.
- Auth-off runtime traffic still carries honest `identified|none|unknown` attribution with per-request `AuthEnforced`. Optional lookup failure permits execution with unknown identity; enforcement failure rejects. A proxy-key 401 is never management session expiry.
- Keep cookies, tokens, request tokens, and key helpers here. Return only metadata or newly generated one-time values and reuse `../../proxykeyusage/` for usage persistence.

Local regression seams are `routes_test.go`, `store_test.go`, `runtime_cache_test.go`, and `runtime_middleware_matrix_test.go`.
