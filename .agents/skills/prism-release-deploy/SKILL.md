---
name: prism-release-deploy
description: Release, package, and deploy Prism through release.sh, GitHub CI, immutable GHCR image manifests, verified backups, and staged Compose rollouts with migration, health, smoke, observation, and stop gates. Use only for explicitly authorized Prism release or deployment work; use prism-ops-inspect for diagnosis-only requests.
metadata:
  short-description: Release and deploy Prism with staged gates
---

# Prism Release Deploy

## Outcome

Carry accepted source through `release.sh`, release-commit CI, an immutable image manifest, and an A-before-B rollout. Complete only after each requested instance passes backup, schema/data, health, applicable management API, and observation gates. Provider traffic and backup pruning are optional, separately authorized operations.

## Authorization

- `plan` is read-only and may inspect repository, GitHub, registry, and deployment state.
- Release execution requires current release/tag/push/package authorization plus `--confirm-release vX.Y.Z`.
- Rollout requires current deployment authorization plus `--confirm-rollout <tag>@<release-sha-prefix>`. Provider traffic additionally requires `--allow-provider-smoke`; prune requires one exact confirmation per service.
- Confirmation flags record authorization but never create it. A current request may authorize release and deployment together, including the required verified backups; continue across covered stages without asking again. Historical tasks provide evidence, not authorization for a new release. Pruning, restore, and real provider traffic require their own explicit scope.

## Preparation

1. Read repository `STATUS.md`, `CONTRIBUTING.md`, and affected-path guides. Inspect Git state and the requested changes; preserve unrelated local work. Merge/commit/push only within the current request's authorization and reach clean, current `main` before executing the release helper.
2. Complete applicable acceptance and regression checks, including an existing GOAL's required cases when the request depends on that GOAL. Record commands, results, and accepted source SHA. Do not create a GOAL just to release.
3. Use `$prism-ops-inspect` for deployment preflight: current image/version, app and PostgreSQL health, migration history, entity counts, bootstrap hash, and backup capacity. Derive expected migrations and feature checks from the target source; prior release versions, counts, and model IDs are not current facts.

## Release stage

1. Run `scripts/prism_release.py plan --spec <patch|minor|major|X.Y.Z>`.
2. Before execution, read [references/release-manifest.md](references/release-manifest.md).
3. Run `scripts/prism_release.py execute --spec <spec> --confirm-release <tag>` once; it invokes repository `release.sh`. Wait for the release commit's CI and tag-triggered Docker Image workflow, not the pre-release commit's CI. Bind accepted source to the release as described in the manifest reference.
4. Verify repository, tag, release SHA, OCI revision/version/platform, and full manifest digest. Write `status: published` evidence under `artifacts/evidence/prism-ops/releases/` only after all gates pass.
5. If the release commit, tag, workflows, and image already published but manifest creation failed, use `prism_release.py recover --spec X.Y.Z --confirm-release vX.Y.Z`. Recovery is validation-only: it requires a clean main containing the release tag, identical local/remote tag identity, green release workflows, and matching OCI evidence; it never reruns `release.sh`, pushes, tags, or rebuilds.

## Rollout stage

1. Run `scripts/prism_rollout.py plan --manifest <published.json> ...`.
2. Read [references/rollout-gates.md](references/rollout-gates.md). For `capy`, also read [../prism-ops-inspect/references/capy.md](../prism-ops-inspect/references/capy.md).
3. Choose orchestration using the helper limitations in the rollout reference before execution. Create a verified backup through `$prism-backup-restore`; deploy A before B, and leave B untouched until A passes every gate. Default to 300 seconds of observation per instance, without provider smoke or pruning.
4. On failure after schema advancement, stop the affected app and preserve its database and backup for an explicit restore decision.

## Completion

- Never create a GitHub Release, rerun failed workflows, force-push, deploy `latest`, use `force` / `down -v`, collapse auxiliary Models, or perform unrelated management writes.
- Recheck both externally reachable health endpoints and save per-instance results plus a combined result under `artifacts/evidence/prism-ops/<tag>/`. Update `STATUS.md` with observed deployment facts and timestamp; commit/push that update only when covered by the current authorization. Keep the immutable release manifest separate from later status commits.
- Lead the handoff with release SHA/tag, CI and image evidence, immutable image reference, per-service backup and migration results, smoke/observation results, stopped or unverified states, and required operator action.
