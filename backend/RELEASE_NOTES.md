# Backend Release Notes

## 2026-04-25 — Runtime concurrency closure Phase 0 baseline

- Supported runtime topology for this rollout is one active Prism backend process per deployment. Multi-active runtime serving is out of scope for the current concurrency-closure work.
- `/v1` and `/v1beta` still rely on 2-second runtime planning and runtime auth TTL caches today, and runtime requests can still reload planning/auth state from PostgreSQL on cache miss or expiry.
- The current runtime hot path still touches `routing_connection_runtime_state`, `routing_connection_runtime_leases`, `loadbalance_round_robin_state`, and synchronous `proxy_api_keys.last_used_*` updates.
- Startup does not publish an initial runtime snapshot yet. After restart, the first runtime request still warms planning/auth state from PostgreSQL on demand instead of failing closed.
- Phase-0 proof artifacts are captured under `artifacts/perf/runtime-concurrency-closure/phase-0/`.
