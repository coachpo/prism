# Rollout gates

## Per-service sequence

1. Re-discover the project and assert the currently running image, health, database schema, config hash, entity counts, backup capacity, and absence of owner/orphan anomalies.
2. Create a verified quiesced PostgreSQL and matching bootstrap backup through `prism_backup.py`. Keep PostgreSQL running while only the app is stopped; leave the app stopped after a successful deployment backup. Save the verified manifest identity before cutover.
3. Deploy the exact image manifest reference. For an app-only release, pull only `prism` and recreate it with `up -d --no-deps prism` using the discovered Compose project and files. Preserve the running PostgreSQL version/container and volume. Re-discover the recreated app before checking health/version and container identity.
4. Require the existing migration history to remain an unchanged prefix, with the target release's expected migrations applied. No-new-migration releases may retain the same history. Check migration-specific effects when applicable (for example index/trigger shape), unchanged bootstrap hash, no loss of preflight entity counts, no owner-backed missing/blank upstream ID, and no false historical backfill. Upstream IDs may legitimately differ from their owner model IDs. For quiesced, no-provider runs compare counts exactly; investigate changes rather than silently accepting a decrease or unexplained growth.
5. Check Requests and applicable feature management APIs, then persist the selected `tag@sha256:…` in the deployment's actual Compose/env source. Back up that source before editing and verify rendered `services.prism.image` without a one-command environment override. Record the source path, hash, previous copy, and image ref. A running container alone does not prove a later Compose invocation will keep this release.
6. Observe for 300 seconds by default, sampling about every 10 seconds including the initial and final checks (the reference runs recorded 31 samples). Require app/PostgreSQL health, expected health-response version, unchanged immutable app image and container identity, and zero restarts. Record actual elapsed time and samples; a sample count alone does not establish duration.
7. Mark this service complete and save its result before starting the next service. For an A/B request, B's backup and mutation start only after A's schema/data, API, persistent-image, and observation gates pass. On resume, verify A's evidence matches the same release and recheck its current health/image before touching B.
8. Only if requested, apply the separately confirmed keep-three retention policy after that service passes. Otherwise record `not_requested` and leave old backups intact. After all requested services pass, recheck both external health endpoints and save a combined result with release/CI/image, backups, per-service gates, and observation timestamps.

## Smoke selection

Default deployment smoke uses health and read-only management behavior; it sends no real provider requests. Select API assertions from the changed feature and current contract, not the HTTP method alone. For example, v1.1.11 checked Requests windows and migration index/trigger effects; v1.1.12 checked OpenCode source/render, exported identities and byte hashes, plus Requests. Its render POST was a non-mutating operation with credentials omitted. Those examples do not make fixed model counts, client versions, or migration IDs universal gates.

When no eligible feature data exists, report that part as unverified (and whether it blocks the requested acceptance); do not edit live metadata merely to make a smoke test pass. A feature's local client acceptance against a controlled upstream is separate evidence from deployed management API smoke.

If real provider smoke is explicitly authorized, require Chat and Responses stream/non-stream visible output plus request/usage attribution, using the host adapter's revalidated profile. Record it separately. Omit `--allow-provider-smoke` otherwise, and report `not_requested`, not `passed`.

## Helper coverage and orchestration

`prism_rollout.py plan --manifest <published.json> --host capy` validates the manifest and prints service order/confirmation parameters; it does not perform the live preflight or verify that all gates below are implemented. `--observe-seconds` defaults to 300. `--confirm-prune <service>:keep-3` is optional and applies only to that selected service.

The generic executor implements backup, image/schema/count/config gates, optional provider smoke, observation, and optional retention. It does **not** provide feature management API hooks, persistent Compose image pinning, or final external-endpoint checks. Its observation loop checks app state/image/restarts and HTTP health; add PostgreSQL health and container-identity checks in the orchestrator. Its shared `deploy_image` helper pulls/recreates the full Compose bundle, so do not invoke that unmodified path for an app-only rollout that must preserve PostgreSQL. It also writes combined evidence only after all services return and rejects B-only execution; do not rely on that alone for resumable per-service evidence.

For the capy practice above, use a reviewed task-scoped orchestrator under `artifacts/evidence/prism-ops/<tag>/` that reuses the verified manifest/backup/gate helpers, restricts cutover to the app, adds the current feature checks and persistent pin, saves per-service outcomes on failure as well as success, and enforces A-before-B. Keep explicit confirmation and stop behavior. Inspect current helper code first; do not blindly replay archived scripts or their string replacements. Old scripts contain version, schema, path, and contract assumptions. These extra gates are required orchestration work, not evidence supplied by a successful generic executor exit.

## Failure behavior

- Release/CI/image failure: no deployment-host writes.
- Backup failure: restart and verify the original app, then stop the rollout.
- A failure: do not touch B.
- Migration failure with unchanged schema: the original image may be restarted only after the schema check proves it remains compatible.
- Health/smoke/observation failure after schema advancement: stop the app, preserve PostgreSQL and backup, and require an explicit restore decision.
- Retry provider calls once only for 429/5xx. Transport errors, 2xx empty visible output, invalid SSE completion, and attribution mismatch stop the rollout.
