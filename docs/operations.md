# Operating Prism

Use the [README quick start](../README.md#quick-start) for first startup. [Deployed instances](#deployed-instances) contains dated deployment observations. The [data policy](../CONTRIBUTING.md#retained-data-and-compatibility-policy) governs retained PostgreSQL and bootstrap state.

## Local Tooling and Build Workflow

- Prism is a monorepo: `backend/` and `frontend/` are root-owned directories that share the root launcher, release helper, and CI wiring.
- Root local orchestration lives in `start.sh`. It first validates `full|headless`, then loads the root `.env` only for variables absent from the invoking shell. Shell-provided values therefore take precedence over `.env`; the script restores its original `PATH` after loading the file.
- The launcher then checks required tools, resolves and exports `PRISM_CONFIG_PATH` (defaulting to repo-local `config.json`) and its local `DATABASE_URL` (`localhost:15432`), builds `backend/prism-backend`, seeds a missing bootstrap file, and validates the selected bootstrap file against the local launcher contract.
- Only after that validation does it bring down the helper compose project, reclaim Prism's backend/frontend ports, verify PostgreSQL port availability, start and wait for the PostgreSQL service from root `docker-compose.yml`, start the backend, and finally start Vite for `full` mode. The backend follows `server.port` in the selected bootstrap file; fresh local seeds use `8000`. PostgreSQL and Vite remain fixed at `15432` and `5173`.
- `./start.sh full` launches the frontend on `5173`, unsets `VITE_API_BASE`, and enables a launcher-local Vite proxy via `PRISM_VITE_PROXY_ENABLED=1` plus `PRISM_VITE_PROXY_TARGET` pointed at that effective backend port so browser traffic stays same-origin.
- Canonical startup config lives in a plaintext bootstrap JSON selected by `PRISM_CONFIG_PATH`; backend-native fresh seeds use listener `0.0.0.0:8000`, localhost/127.0.0.1 CORS origins on `5173`, and `runtime.sideEffects.attemptTimeout="10s"`; the database URL defaults to `postgres://prism:prism@localhost:5432/prism?sslmode=disable` unless `DATABASE_URL` is set, while `start.sh` sets `DATABASE_URL` to the local launcher PostgreSQL DSN on host port `15432` before seeding.
- The root `Dockerfile` plus root `docker-compose.yml` are the default local/self-hosted bundle: Compose builds one Prism app image, runs PostgreSQL as a separate service, and the app image always includes the Go backend, migrations, version metadata, React assets, and Nginx. Nginx's `PRISM_BACKEND_UPSTREAM_PORT` is a static environment substitution; it is not derived from the bootstrap file. Any container deployment that changes bootstrap `server.port` must set `PRISM_BACKEND_UPSTREAM_PORT` to the same port.
- The root app image runs as `prism:prism`, UID/GID `1000:1000`. Container deployments that bind mount `/app/config` or any other `PRISM_CONFIG_PATH` parent must make that host directory writable by UID/GID `1000:1000`; new and existing root-owned mounts should be prepared once with `sudo chown -R 1000:1000 <prism-config-dir>` and `sudo chmod 0700 <prism-config-dir>`.
- Bootstrap JSON is strict v1: `meta.schemaVersion` must be `1`, unknown fields are rejected, and `runtime.sideEffects.attemptTimeout` is a required positive duration. The `runtime.transport` config section was removed outright (no compatibility shell); a leftover `runtime.transport` block is rejected with a readable migration error rather than a bare unknown-field error. Legacy encrypted bootstrap fields (`secretPayload`, `database.urlSecretRef`, and `auth.jwtSigningKeySecretRef`) are rejected rather than migrated at boot. Missing files are seeded once, and the entrypoint has a narrow repair path for stale files rejected only because they contain retired `docsEnabled`; other invalid legacy shapes fail validation.
- Safe bootstrap snapshots expose non-secret values and secret metadata only. They never return secret material; `runtime.secretEncryptionKey` is preserve-only in v1; no management API edits the bootstrap document. File-backed startup edits require a process restart because Prism does not watch external config changes. Existing valid files are preserved until an operator stops Prism, removes or relocates the file, and restarts.
- `database.pools.realtime`, `auth.resetCodeTtlSeconds`, `runtime.routing.openaiTerminalTranslationMode`, `stateTransfer.bundleEncryptionKey`, `mail`, and `telemetry` remain parse-and-validate compatibility fields for live `config.json` files. They do not restore the removed realtime pool, reset-code TTL override, terminal-translation mode, state transfer, mail delivery, or telemetry exporter processes.
- The backend does not mount a local `/metrics` scrape endpoint.
- Request-history APIs and settings-page state flows remain PostgreSQL-backed product state instead of bootstrap telemetry ownership.
- Disaster recovery is handled outside the dashboard with `pg_dump` plus a copy of the plaintext startup config.
- `.github/workflows/docker-images.yml` publishes only `ghcr.io/<owner>/<repo>` from the root Dockerfile for `linux/arm64`, using a native ARM64 runner. Tag pushes require a green CI conclusion for the tagged commit; manual dispatch publishes a `manual-<sha>` image, while only release tags move `latest`. `.github/workflows/cleanup.yml` prunes untagged versions of that single package; its safety assumption is the current single-platform manifest with provenance and SBOM manifests disabled.

## Startup Config File

Prism loads steady-state startup settings from the plaintext `config.json` selected by `PRISM_CONFIG_PATH`. The live v1 file requires `meta`, `server`, `database.url`, `database.pools`, `database.managementAdmission`, `runtime.secretEncryptionKey`, `runtime.sideEffects`, `http.corsAllowedOrigins`, and `auth`. Optional top-level sections are `alerting`, `mail`, `telemetry`, and `stateTransfer`; `mail`, `telemetry`, and `stateTransfer` are parsed for compatibility only and do not re-enable retired behavior. The `runtime.transport` section was removed outright and is rejected with a readable migration error when present. The management API does not edit this file; external edits require a Prism restart before they affect the running process.

## Configuration Persistence

- Runtime and management configuration is stored in PostgreSQL with Go-backend-managed schema migrations applied at startup.
- Startup/bootstrap process settings are owned by the plaintext `config.json` bootstrap file; external edits require a Prism restart.
- The default profile exists from the first startup and remains editable after initialization.
- Database setup is managed by the Go backend runtime and applies the checked-in fresh-install baseline on startup.

## Deployment Inspection

Run `docker compose ps` from the directory and Compose project used to start the instance. Check both `prism` and `postgres`, then request `/health` through the actual published app port (default `8080`). The app response reports liveness and completed startup. Check PostgreSQL separately through its Compose health check. The app health response does not exercise a provider or run a new database query. Check startup logs with `docker compose logs --tail 100 prism` when the service is unhealthy or a schema or config gate fails.

Record the actual app image identity and the latest applied entry in `prism_schema_migrations` when comparing a target instance with its expected version. Keep credentials and plaintext bootstrap files out of diagnostic output. Start with the [priority and routing diagnosis](#diagnose-routing-and-observability) for routing availability, management overload, scheduler lag, or gaps in retained history.

## Deployed Instances

Observation-time facts, not continuous health guarantees. Both instances were deployed and verified on 2026-09-25 at 00:57 UTC through the `capy` SSH adapter, selected Docker/Compose fields, PostgreSQL metadata, and the health, Requests, `/v1/models`, and loadbalance strategy list and preview APIs:

| Instance | Dashboard | App / PostgreSQL health | App version | Latest migration |
| --- | --- | --- | --- | --- |
| `prism-a` | [Observe A](http://192.168.1.222:8087/observe?tab=overview&metric=requests&scope=ingress&group_by=none&interval=auto) | healthy / healthy | 1.1.21 | `000035_request_scoped_reroute` |
| `prism-b` | [Observe B](http://192.168.1.222:8088/observe?tab=overview&metric=requests&scope=ingress&group_by=none&interval=auto) | healthy / healthy | 1.1.21 | `000035_request_scoped_reroute` |

Both run PostgreSQL 16.15 and the same app image, `ghcr.io/coachpo/prism:v1.1.21@sha256:5a33dc570a0aad6b5b8711b5bf83fdd29de15159c3f2762c9be01a0114c7c6c8`, pinned in each instance's Compose env file, with independent databases and bind-mounted plaintext bootstrap files. Only the app containers were recreated; PostgreSQL kept running. Both app restart counters were zero at observation. Quiesced PostgreSQL and matching bootstrap backups were verified before each update; schema history, entity counts, and config hashes were preserved. Each instance passed a 300-second observation with 30 samples. No real-provider smoke was performed. Migration `000035_request_scoped_reroute` was applied by v1.1.18, deployed earlier that day and superseded by v1.1.20 and then v1.1.21; each upgrade kept the preceding schema history as a prefix. Tags `v1.1.16` and `v1.1.19` have no published image and were never deployed.

The repository supports the root Compose app-plus-PostgreSQL bundle, the single app image with an external PostgreSQL, and the local `start.sh` launcher. The [README](../README.md#quick-start) owns ordinary startup; [Local Tooling and Build Workflow](#local-tooling-and-build-workflow) owns the packaging model.

## Back Up, Upgrade, and Recover

Back up the database with `pg_dump` and copy the matching plaintext `config.json` from the selected `PRISM_CONFIG_PATH`. For retained instances, quiesce Prism while leaving PostgreSQL available so the pair represents the same state. Keep the config and dump private. Verify the backup by restoring the database to a disposable PostgreSQL instance and checking it with the matching bootstrap configuration, using an isolated database URL for the rehearsal. Keep the original config copy with the dump.

Use the [release and image convention](../CONTRIBUTING.md#releases) to identify the versioned image. The [changelog](../CHANGELOG.md) records tags, including versions known to have no app image. For an existing instance, record the exact current image before replacing it, retain its volumes, and compare the expected migration sequence against the instance's state and the [startup migration boundaries](data-model.md#startup-migrations-and-retained-state). Check app and database health, migration history, and the affected behavior after replacement. The `000023_pricing_template_kind_cards` migration refuses retained legacy pricing or currency-migration rows. See the [retained-data policy](../CONTRIBUTING.md#retained-data-and-compatibility-policy) before any rebuild.

An older image may reject a newer database schema. For recovery across an incompatible upgrade, rehearse the previous image against the verified backup pair before switching the instance back. The repository's current Compose and release helpers do not provide an automated backup, restore, or rollout command.

## Direct-Request Entry Reclassification

This tracked operator bundle is the one-off Default-profile data change that
follows the generic `000032_model_direct_request_enabled.sql` migration. It is
shipped for explicit operator use but is never invoked by startup. The release
migration intentionally keeps every retained model directly requestable; this
runbook applies the operator-approved 12/4 classification without turning the
instance IDs into a generic migration rule.

The executable companion is
`scripts/operations/direct-request-entry-reclassification.sql`. It defaults to a
read-only, serializable preview. Apply mode is impossible unless the operator
supplies the exact apply, verified-backup, quiesce, and database-name tokens.
The runbook does not establish whether either mode has been executed on an instance; use retained execution evidence for that fact.

The runbook is bound to the reviewed SQL payload:

```text
SQL SHA-256: d4afecb631dc70fa974d96cf0a877958ee3187c64675092260c6729b0fd396d0
```

The canonical disposable acceptance test is
`backend/tests/integration/direct_request_entry_reclassification_plan_test.go`.
It requires this runbook, the SQL, and the Go test to be visible to
`git ls-files`, verifies the SQL hash above, and requires `CONTRIBUTING.md` to
describe the bundle as shipped rather than ignored or unshipped.

### Accepted identity set

Direct entries (exact and case-sensitive):

```text
codex/codex-auto-review
codex/gpt-5.6-terra
deepseek-v4-flash
deepseek-v4-pro
glm-5.3-flash
codex/gpt-image-2
codex/gpt-5.4-mini
codex/gpt-5.5
codex/gpt-5.6-luna
gpt-5.6-luna
muse-spark-1.2-contributor
qwen3.8-flash
```

The approved spelling map is recorded as non-entry identity → direct entry
identity:

```text
DeepSeek-V4-Flash              → deepseek-v4-flash
deepseek/deepseek-v4-flash-0731 → deepseek-v4-flash
deepseek/deepseek-v4-pro        → deepseek-v4-pro
z-ai/glm-5.3-flash              → glm-5.3-flash
```

Model Target edges use the entry/parent as `source_model_config_id` and the
non-entry spelling as `target_model_config_id`:

```text
deepseek-v4-flash --Model Target--> DeepSeek-V4-Flash
deepseek-v4-flash --Model Target--> deepseek/deepseek-v4-flash-0731
deepseek-v4-pro --Model Target--> deepseek/deepseek-v4-pro
glm-5.3-flash --Model Target--> z-ai/glm-5.3-flash
```

The four right-hand IDs become `direct_request_enabled=false`. Existing edges
are retained byte-for-byte, including ID, position, and enabled state. A
missing edge is appended enabled at the end of its parent’s mixed target list,
with the two Flash children using the order shown above. No model or connection
`is_enabled`, Terminal Target, connection ownership, upstream identity, log,
usage event, pricing row, or directory binding is changed.

### Reclassification safety boundary

Apply requires all of the following:

1. Deploy the schema/code upgrade and confirm migration 000032 succeeded.
2. Create and validate a current Prism PostgreSQL plus plaintext-config backup.
3. Stop the Prism application while leaving PostgreSQL available. Keep it
   stopped for the whole transaction; do not permit concurrent management
   writes.
4. Resolve the exact target database name and use an operator connection with
   access only to that database.
5. Save preview output and compare it with the approved 16 IDs and four edges.

The SQL takes stable row locks in apply mode and validates every condition
before its first persistent write: the exact 16-row set, Default profile,
000032 schema and migration-history stamp, parent/child family and both OpenAI
dimensions, edge uniqueness and enabled state, dense affected-parent positions,
prospective acyclicity, and the three enabled Flash logical/upstream identities.
Any mismatch aborts the serializable transaction with zero persistent writes.

The application’s HTTP middleware is normally the sole generation writer.
Because this is a quiesced offline batch, the SQL performs the equivalent four
planning-generation bumps and one route-witness-generation bump in the same
transaction, but only when at least one model bit or edge actually changes.
Restarting Prism after commit discards every old in-memory snapshot. A true
second run changes neither business rows nor generations.

### Reclassification preview

With Prism still running, preview is read-only and requires no confirmation
tokens:

```bash
psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -f scripts/operations/direct-request-entry-reclassification.sql
```

The preview rolls back. It prints all 16 current/desired entry bits, all four
correct parent-to-child edges with existing ID/position/state or `will_append`,
and the exact Flash Terminal Target identities. Stop on any SQL exception or
unexpected row.

### Reclassification apply

After the backup is verified and Prism is stopped, substitute the exact
database name shown by `SELECT current_database()`:

```bash
psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v apply_token=APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1 \
  -v backup_token=BACKUP_VERIFIED \
  -v quiesce_token=PRISM_STOPPED \
  -v expected_database=prism \
  -f scripts/operations/direct-request-entry-reclassification.sql
```

The transaction updates only mismatched qualification bits and inserts only
missing approved edges. It then proves `is_enabled` and every pre-existing
target row stayed unchanged, proves the exact qualification/edge postcondition,
bumps generations if and only if business rows changed, and commits. The final
summary is instance-dependent: it reports one `model_qualification` row for
each bit that actually changes and one `model_target_append` row for each
missing edge. Use the preview for the target instance to establish which of the four edges already exist.

### Reclassification post-commit verification

1. Run the preview again before restart. Every `will_update` and `will_append`
   value must be false; apply mode would report no change rows and would not
   advance generations.
2. Restart Prism only after that no-op preview passes.
3. Read `/api/models`: the direct set must be exactly the 12 IDs above and the
   other four rows false; compare all model `is_enabled` values with the saved
   preview.
4. Read `/v1/models` and Pi export source: neither may include the four
   non-entry IDs.
5. Read model/setup readiness: only direct roots count, while the parent model
   diagnostics still traverse each non-entry child.
6. Confirm `deepseek-v4-flash` retains its own Terminal Target plus the two
   Model Targets and that the three exact upstream identities remain
   `deepseek-v4-flash`, `DeepSeek-V4-Flash`, and
   `deepseek/deepseek-v4-flash-0731`.
7. Do not send a real provider request merely to validate this migration.

Save preview/apply/no-op output and before/after inventory checks under
the task's `.agent-work/<task-slug>/evidence/` directory. If any postcondition fails, leave Prism stopped and use
the verified backup/restore procedure; do not improvise another SQL repair.

### Disposable acceptance runner

The canonical Go test executes these exact SQL bytes through `psql` against the
integration suite's shared disposable PostgreSQL and real migrated template. It
uses `psql` inside the harness container by default, falling back to the host
client only for an explicitly external harness without a container name. It
never reads the home-LAN database or opens port 8088. Its 19 table-driven
scenarios verify source binding plus preview, existing-edge and missing-edge
success, a true second-apply no-op, and every precondition, inventory,
compatibility, edge-state, position, cycle, and DeepSeek-identity failure with
byte-equivalent business and generation snapshots:

```bash
cd backend
go test -timeout 30m ./tests/integration \
  -run '^TestDirectRequestEntryReclassificationPlan$' -count=1
```

Retain acceptance output under the task's `.agent-work/<task-slug>/evidence/` directory when an execution record is needed. This Go test is acceptance support, not a production migration command.

## Client Configuration Export Quick Guide

Open **模型与服务 → 模型 → 导出客户端配置**. Pi remains the default; choose **OpenCode** for OpenCode **1.18.27**. Enter the Prism gateway origin and a provider key (`prism`, or `prism-` followed by a lowercase letter or digit and then lowercase letters, digits, `_` or `-`). Select the eligible text entries and inspect **查看资料与来源** before generating `opencode-prism.json`.

OpenCode requires valid positive context and output limits. Missing or invalid limits block that model; open **补全此模型的资料** in that row to bind or correct its saved values, then return to export after the source refreshes. Enter only limits supported by reliable model facts. Export never fetches a catalog or borrows Pi metadata. Prices are included only when every reachable target has the same complete standard USD price per million tokens and the reasoning rate equals the output rate. Tiered, peak/valley, missing or conflicting prices omit the whole cost group; OpenCode's possible zero estimate does not mean the model is free.

**不嵌入密钥** writes `env: ["PRISM_API_KEY"]` and omits `apiKey`. With OpenCode **1.18.27** on `PATH`, run from the project directory whose OpenCode configuration you want to load. Replace the key placeholder when Prism requires a proxy key, and use the absolute path to the downloaded file.

```bash
export PRISM_API_KEY='replace-with-your-prism-proxy-key'
OPENCODE_CONFIG=/absolute/path/opencode-prism.json opencode
```

Alternatively, enter a final key in the export dialog; the downloaded file then carries it explicitly. In OpenCode select `prism/<complete Prism model ID>` (for example `prism/codex/gpt-x` when that exact entry exists). A custom provider key replaces only the initial `prism` segment. Chat Completions, Responses (including `dual_native`), Anthropic Messages and Gemini use model-specific SDKs and URLs inside the same provider.

To merge into an existing `opencode.json`, choose **复制 provider 合并片段** and paste the fragment into the singular `provider` object, preserving other provider keys. Pi's corresponding fragment belongs under plural `providers`. In Pi, repair an unready row's binding first, then explicitly select it once backend readiness is confirmed. `OPENCODE_CONFIG` participates in OpenCode's normal configuration merge; it does not isolate existing user/project settings. Use a distinct provider key and check that existing settings do not override its models, credentials or URLs. Prism never edits client files or sets a default model automatically.

The [OpenCode format example](../backend/internal/domain/modelexport/testdata/opencode_prism.golden.json) contains synthetic model metadata and no real key. Generate from your own Prism models for use. Gemini IDs that cannot survive its URL path grammar are explicitly unavailable for export; complete IDs containing `/` are retained for the body-based OpenAI and Anthropic protocols. Client inference defaults are not authored reasoning adaptations: custom variants, SDK options and interleaved reasoning are outside this export.

## Everyday Investigation and Maintenance

In model detail, open **检查当前可用连接 → 检查连接** to inspect current published paths and exclusion reasons. The sample does not send a request and does not promise the next target. In **请求记录**, choose ingress duration or cost ordering, open the complete chain, and select two requests for metadata and explicitly loaded capture comparison. Cost groups remain separate; absent usage or price is never treated as free.

The Models page offers bounded batch limit overrides, strategy assignment and target price-template assignment. Select objects, review every difference, explicitly confirm manual replacements, and apply. A conflict requires a new preview and leaves the whole batch unchanged.

Save client export combinations or request views by name and use their JSON file import/export to move to another browser. Reconcile stale or instance-specific references before restoring. These files exclude credentials and request bodies; if browser storage fails, download the file to retain the preference.

On a phone, start with the request summary and enter the full investigation. Observe's **近期活动** lists retained finalized ingress requests. Automatic refresh is off by default; choose 30 or 60 seconds when needed. Background tabs pause, outstanding reads prevent overlapping refreshes, and each fragment shows its own sampling time and stale state.

## Diagnose Routing and Observability

Open **连接状态** at `/observe/routing-health`. Its Current State shows the live process. The Events timeline uses its own retained time window and reports coverage gaps. Use [priority enforcement and failure modes](priority-enforcement.md) to interpret admission responses, lane pressure, worker lag, durable telemetry, best-effort feedback, and cache-generation failures. The [backend regression commands](../CONTRIBUTING.md#tests-checks-and-builds) include the priority suites for local validation.
