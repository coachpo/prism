# capy adapter

Use this adapter only when the requested deployment host is `capy` or the request names `prism-a` / `prism-b`.

## Expected topology

- SSH alias: `capy`.
- Compose projects: `prism-a`, `prism-b`.
- Each project owns services `prism` and `postgres`, an independent PostgreSQL volume, and a bind-mounted `/app/config` directory.
- The deployment repository has historically lived below `/home/ubuntu/orange_work/curse`, with a root `deploy.sh`; discover current `ConfigFiles`, parent root, env file, mounts, ports, and containers from Compose/Docker before using any path.
- Expected public ports are A `8087`, B `8088`, and B PostgreSQL `8432`; they are assertions to check, not values to inject.

## Live smoke profile

When provider smoke is separately authorized, the current profile is:

- Chat Completions: `deepseek-v4-flash`, stream and non-stream, `max_tokens=256`.
- Responses: `codex/gpt-5.5`, stream and non-stream, `max_output_tokens=32`.

Require non-empty visible output, a normal stream terminal event, persisted request/usage attribution, and a non-empty allowed `upstream_model_id`. Retry only one 429/5xx. A 2xx response with reasoning-only output fails the smoke gate.

Do not encode the current Prism version, image digest, container ID, database name, backup timestamp, config hash, or row counts in this adapter.
