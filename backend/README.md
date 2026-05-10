# Prism Backend

**Go backend runtime for proxy routing, load balancing, telemetry, sidecar control-plane sync, and management APIs.**

This directory owns Prism's live Go backend service.

## Project structure

```text
backend/
├── cmd/prism-backend/              # Go process entrypoint
├── internal/httpapi/               # management, sidecars, runtime, realtime, and docs handlers
├── internal/platform/              # config, server assembly, migrations, startup, workers, version
├── internal/domain/                # audit, loadbalance, and stats domain logic
├── internal/{endpoint,profile,vendor}domain/ # shared management-domain helpers
├── migrations/                     # SQL migration chain applied by the Go runtime
├── testdata/                       # checked-in OpenAPI, bundle, and realtime fixtures
├── tests/                          # Go contract, integration, and runtime regressions
├── docker-compose.yml              # local PostgreSQL provisioning
├── Dockerfile                      # Go backend image build
└── VERSION                         # backend version surface
```

## Running

Recommended local entrypoints:
```bash
../start.sh headless
../start.sh full
```

When launched through `../start.sh`, the backend listens on `http://localhost:18000`.

If the selected bootstrap config keeps docs enabled, it also serves:
- Swagger UI: `http://localhost:18000/docs`
- ReDoc: `http://localhost:18000/redoc`
- OpenAPI JSON: `http://localhost:18000/openapi.json`

Direct Go runs from `backend/` use `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`. The only optional startup env vars are `PRISM_CONFIG_PATH` and `DATABASE_URL`, and the default database URL is `postgres://prism:prism@localhost:5432/prism?sslmode=disable`.

## Verification

Use the Go regression packages directly for targeted validation:

```bash
go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
go test ./...
go build ./cmd/prism-backend
```

## Configuration
- Supported steady-state backend startup uses `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`.
- When the bootstrap file already exists, Prism loads startup settings from it and the legacy app env surface is not the supported source of truth.
- When the bootstrap file is missing, Prism seeds it from built-in defaults plus the optional `DATABASE_URL` input only.
- The startup bootstrap contract is not DB-backed, and profile backup/restore, vendor catalog export/import, global log retention, and other settings-page state flows remain PostgreSQL-backed state transport.
- `../start.sh` reads the root `../.env`, provisions local PostgreSQL, defaults `PRISM_CONFIG_PATH` to `../config.json`, and seeds that plaintext bootstrap file when it is missing so local runs keep backend `18000` and the local PostgreSQL DSN on host port `5432`.
- Before booting, `../start.sh` verifies that the selected bootstrap file still resolves to the local launcher contract instead of trying to negotiate alternate backend ports or database targets.
- Direct Go runs should prefer an absolute `PRISM_CONFIG_PATH`.
- The backend container image runs as `prism:prism`, UID/GID `1000:1000`. If `PRISM_CONFIG_PATH` points inside `/etc/prism`, bind mount the containing host directory, such as `/absolute/secure/path/prism-config:/etc/prism:rw`, and make that directory writable by UID/GID `1000:1000`.
- Prepare new host config directories with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`. Use the same one-time remediation for existing root-owned bind mounts before starting the non-root backend image.
- Bootstrap writes are file-durable. Eligible hot fields apply immediately when written through the Startup tab or `PUT /api/config/bootstrap`; structural fields remain pending until restart.
- Hot fields include CORS origins, auth TTL and cookie metadata, mail and SMTP settings, runtime buffering and transport settings, and M2/M3 management admission limits.
- Restart-required fields include listener host and port, docs enablement, database URL and pool budgets, runtime side-effects attempt timeout, runtime secret encryption key, auth JWT signing key, and state-transfer bundle key.
- External edits to the bootstrap file are not watched automatically. Use the Startup tab or `PUT /api/config/bootstrap` to publish hot-eligible file edits into the running process.
- The bootstrap API stays file-backed only, so `/api/config/bootstrap` is separate from PostgreSQL-backed settings flows.
- Raw bootstrap files require `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout` as Go duration strings. Set `runtime.transport.requestTimeout` to `"60s"` to keep the prior whole-request upstream timeout behavior. Set `runtime.sideEffects.attemptTimeout` to the newly seeded default `"10s"` for each background side-effect enqueue attempt. Missing either required field fails startup validation by design.
- `runtime.sideEffects.attemptTimeout` is restart-required and not hot-applied. It is separate from `runtime.transport.requestTimeout`, which remains the whole-request upstream provider HTTP timeout.
- Auth email delivery is disabled when `mail` is missing or `mail.enabled=false`; disabled mode uses no-op delivery and does not dial SMTP.
- To enable SMTP, set `mail.enabled=true`, `mail.from`, and `mail.smtp` through the Startup tab or API PUT. Enabled-but-invalid SMTP config fails validation or startup instead of silently falling back to no-op delivery.
- SMTP config fields are `mail.from`, `mail.replyTo`, `mail.smtp.host`, `mail.smtp.port`, `mail.smtp.mode`, `mail.smtp.ehloHostname`, `mail.smtp.auth`, `mail.smtp.username`, `mail.smtp.password`, `mail.smtp.passwordFile`, `mail.smtp.timeout`, and `mail.smtp.tlsServerName`.
- Supported `mail.smtp.mode` values are `starttls_required`, `implicit_tls`, and `plaintext_local_only`. `plaintext_local_only` is local or loopback only, and auth over non-local plaintext is forbidden.
- `mail.smtp.auth` accepts `none` or `plain`. `plain` requires `username` plus exactly one of `password` or `passwordFile`; `passwordFile` is preferred for deployed secrets.
- `mail.smtp.password` is secret-managed by the bootstrap API as `mail.smtp.password`. Safe responses return only metadata, and updates must use preserve or replace secret actions.
- Roll back real delivery by removing `mail` or setting `mail.enabled=false` through the Startup tab or API PUT. Direct external file edits take effect only after restart unless they are later published through the API.
- Local and automated tests use fake or no-op SMTP only. Do not use external SMTP credentials in regression tests.

Enabled SMTP bootstrap example:

```json
{
  "mail": {
    "enabled": true,
    "from": "Prism <noreply@example.com>",
    "replyTo": "support@example.com",
    "smtp": {
      "host": "smtp.example.com",
      "port": 587,
      "mode": "starttls_required",
      "ehloHostname": "prism.example.com",
      "auth": "plain",
      "username": "smtp-user",
      "passwordFile": "/run/secrets/prism-smtp-password",
      "timeout": "15s",
      "tlsServerName": "smtp.example.com"
    }
  }
}
```

## Database and docs artifacts
- Schema migrations are Go-managed and applied from `migrations/` at startup.
- Startup now fails fast when an existing database has application tables but no `prism_schema_migrations` history; reset incompatible local databases instead of relying on migration cutover bridges.
- Request telemetry, usage attribution, audit rows, and load-balance history live in PostgreSQL partitioned log tables.
- Normal log retention is global across all profiles. Configure it through `/api/settings/log-retention` and run it through durable `log_retention` jobs from `POST /api/maintenance/log-retention/jobs`.
- Retention drops whole daily child partitions whose upper bound is `<= cutoff`. Only the cutoff-overlapping boundary child receives bounded cleanup plus `VACUUM (ANALYZE, PROCESS_TOAST TRUE)`.
- Audit rows keep weak request references through `request_log_id`, `request_log_created_at`, and `ingress_request_id`; request detail links can be missing after request-log retention expires first.
- The partitioned-log upgrade is a clean break for old log rows. Old log rows are not preserved.
- `VACUUM FULL`, `CLUSTER`, and `pg_repack` are manual or emergency shrink tools only, not automatic retention steps. The default local `postgres:16-alpine` database does not include `pg_repack`.
- `docs/openapi.json` is the checked-in management and health contract that the Go server serves at `/openapi.json`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d postgres` from `backend/`.
