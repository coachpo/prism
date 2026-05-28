# Prism Backend

**Go backend runtime for proxy routing, load balancing, telemetry, sidecar control-plane sync, and management APIs.**

This directory owns Prism's live Go backend service.

## Project structure

```text
backend/
├── cmd/prism-backend/              # Go process entrypoint
├── internal/httpapi/               # management, sidecars, runtime, realtime, and shared handler seams
├── internal/platform/              # config, server assembly, migrations, startup, workers, version
├── internal/domain/                # audit, loadbalance, and stats domain logic
├── internal/{endpoint,profile,vendor}domain/ # shared management-domain helpers
├── migrations/                     # fresh-install SQL baseline applied by the Go runtime
├── testdata/                       # bundle, request, bootstrap, and realtime fixtures
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

When launched through `../start.sh`, the backend listens on the selected bootstrap file's port — `http://localhost:18000` with the checked-in `../config.json` — and the frontend is served on `http://localhost:5173` in full mode.

Direct Go runs from `backend/` use `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`. The only optional startup env vars are `PRISM_CONFIG_PATH` and `DATABASE_URL`, and the default database URL is `postgres://prism:prism@localhost:15432/prism?sslmode=disable`.

## Runtime proxy contract

The backend mounts runtime handlers under `/v1` and `/v1beta`, but Prism does not support every route under those prefixes. Runtime ingress is operation-led: the operation registry resolves the exact method and path before body reads, request planning, provider transport, telemetry, audit, or feedback side effects. Unsupported vendor routes are rejected instead of being treated as generic passthrough.

Supported runtime routes are:

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1beta/models/{model}:generateContent`
- `POST /v1beta/models/{model}:streamGenerateContent`
- `POST /v1beta/models/{model}:countTokens`

After registry resolution, all supported operations share the same execution core for active-profile model access resolution, load-balance planning, upstream forwarding, and runtime telemetry. Ordered access targets resolve to final standalone connections or same-family model targets before execution, and operation hooks own request extraction, non-stream response parsing, stream terminal classification, and media or multipart handling around that shared core. Prism is a focused proxy for these operations, not a full vendor API clone.

## Verification

Use the Go regression packages directly for targeted validation:

```bash
go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
go test ./...
go build ./cmd/prism-backend
```

## Configuration
- Supported steady-state backend startup uses `PRISM_CONFIG_PATH` and a plaintext bootstrap file such as `../config.json`.
- Backend-owned canonical defaults are the source of truth for freshly seeded bootstrap files: server `0.0.0.0:8000`, CORS for `http://localhost:5173`, PostgreSQL pool total `24` with split `4/8/4/2/2/2/2`, transport `100/16/16/300s/90s/0s/10s/1s`, side-effect timeout `10s`, and management admission `3/2`.
- When the bootstrap file already exists and is valid, Prism loads startup settings from it without rewriting it, even if it contains older values.
- When the bootstrap file is missing, Prism seeds it from backend-owned defaults plus the optional `DATABASE_URL` input only.
- The startup bootstrap contract is not DB-backed, and profile backup/restore, vendor catalog export/import, global log retention, and other settings-page state flows remain PostgreSQL-backed state transport.
- `../start.sh` reads the root `../.env`, provisions local PostgreSQL, defaults `PRISM_CONFIG_PATH` to `../config.json`, and seeds that plaintext bootstrap file only when it is missing so local runs keep frontend `5173` and the local PostgreSQL DSN on host port `15432`; fresh seeds default backend port to `8000`.
- Before booting, `../start.sh` verifies that the selected bootstrap file keeps the local launcher host and database contract, then uses that file's configured backend port. If an existing valid file still carries old-but-valid values, reset manually by stopping Prism, removing or relocating the bootstrap file, and restarting.
- Direct Go runs should prefer an absolute `PRISM_CONFIG_PATH`.
- The backend container image runs as `prism:prism`, UID/GID `1000:1000`. If `PRISM_CONFIG_PATH` points inside `/app/config`, bind mount the containing host directory, such as `/absolute/secure/path/prism-config:/app/config:rw`, and make that directory writable by UID/GID `1000:1000`.
- Prepare new host config directories with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`. Use the same one-time remediation for existing root-owned bind mounts before starting the non-root backend image.
- Bootstrap writes are file-durable. Eligible hot fields apply immediately when written through the Startup tab or `PUT /api/config/bootstrap`; structural fields remain pending until restart.
- Hot fields include CORS origins, auth TTL and cookie metadata, mail and SMTP settings, runtime transport settings, and M2/M3 management admission limits. Runtime buffering is internal and not exposed through bootstrap config.
- Restart-required fields include listener host and port, database URL and pool budgets, runtime side-effects attempt timeout, runtime secret encryption key, auth JWT signing key, and state-transfer bundle key.
- External edits to the bootstrap file are not watched automatically. Use the Startup tab or `PUT /api/config/bootstrap` to publish hot-eligible file edits into the running process.
- The bootstrap API stays file-backed only, so `/api/config/bootstrap` is separate from PostgreSQL-backed settings flows.
- Raw bootstrap files require `runtime.transport.requestTimeout` and `runtime.sideEffects.attemptTimeout` as Go duration strings. Fresh seeds set `runtime.transport.requestTimeout` to `"300s"` and `runtime.sideEffects.attemptTimeout` to `"10s"`. Missing either required field fails startup validation by design.
- `runtime.transport.requestTimeout` remains the whole-request upstream provider HTTP timeout and is hot-applicable through the Startup tab or API. `runtime.sideEffects.attemptTimeout` is restart-required and not hot-applied.
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

## Database and runtime data
- Fresh-install schema setup is Go-managed and applied from the single checked-in baseline under `migrations/` at startup.
- Prism supports empty PostgreSQL databases and databases already stamped with the current `prism_schema_migrations` baseline. Databases with application tables but missing current baseline history fail fast; reset incompatible local databases instead of expecting startup to rewrite historical schemas.
- Pricing and token contract changes use the same clean cut local-data stance: reset and recreate incompatible local data, with no backfill path for old pricing or token semantics.
- For local-only manual testing where a PostgreSQL reset is simpler than remediating a hand-edited database, stop Prism and run `docker compose down -v` from `backend/`, then `docker compose up -d prism-postgres` or `../start.sh headless` to recreate the local database. This deletes local PostgreSQL data.
- Request telemetry, usage attribution, audit rows, and load-balance history live in PostgreSQL partitioned log tables.
- Normal log retention is global across all profiles. Configure it through `/api/settings/log-retention` and run it through durable `log_retention` jobs from `POST /api/maintenance/log-retention/jobs`.
- Retention drops whole daily child partitions whose upper bound is `<= cutoff`. Only the cutoff-overlapping boundary child receives bounded cleanup plus `VACUUM (ANALYZE, PROCESS_TOAST TRUE)`.
- Audit rows keep weak request references through `request_log_id`, `request_log_created_at`, and `ingress_request_id`; request detail links can be missing after request-log retention expires first.
- `VACUUM FULL`, `CLUSTER`, and `pg_repack` are manual or emergency shrink tools only, not automatic retention steps. The default local `postgres:16-alpine` database does not include `pg_repack`.

For local PostgreSQL provisioning without the root launcher, run `docker compose up -d prism-postgres` from `backend/`.
