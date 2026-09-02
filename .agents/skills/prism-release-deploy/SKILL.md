---
name: prism-release-deploy
description: Release, package, and deploy Prism through release.sh, GitHub CI, immutable GHCR image manifests, verified backups, and staged Compose rollouts with migration, health, smoke, observation, and stop gates. Use only for explicitly authorized Prism release or deployment work; use prism-ops-inspect for diagnosis-only requests.
metadata:
  short-description: Release and deploy Prism with staged gates
---

# Prism Release Deploy

Use a two-stage contract: publish an immutable release manifest, then consume that manifest for rollout. Authorization for one stage does not authorize the other.

## Release stage

1. Run `scripts/prism_release.py plan --spec <patch|minor|major|X.Y.Z>`.
2. Before `execute`, require explicit release/tag/push/package authorization and the exact `--confirm-release vX.Y.Z` token.
3. Execute the repository `release.sh`, then wait for the release commit's CI and tag-triggered Docker Image workflow. Do not reuse the pre-release commit's CI.
4. Verify OCI revision, version, OS, architecture, and manifest digest. Write a `status: published` manifest under `artifacts/evidence/prism-ops/releases/` only after every gate passes.

Read [references/release-manifest.md](references/release-manifest.md) before release execution.

## Rollout stage

1. Run `scripts/prism_rollout.py plan --manifest <published.json> ...`.
2. Require explicit deployment authorization and `--confirm-rollout <tag>@<release-sha-prefix>`. Provider traffic additionally requires `--allow-provider-smoke`. Retention requires one exact prune confirmation per service.
3. Back up each service through the sibling `$prism-backup-restore` script, then deploy A before B. B is untouched until A passes every configured gate.
4. On migration/health/smoke failure after schema advancement, stop the current app and preserve the database and backup. Never start an older binary or restore data without a new restore authorization.

Read [references/rollout-gates.md](references/rollout-gates.md). For `capy`, also read [../prism-ops-inspect/references/capy.md](../prism-ops-inspect/references/capy.md).

## Boundaries

- `plan` is read-only. `execute` flags are mechanical evidence of current-task authorization, not substitutes for obtaining it.
- Do not create a GitHub Release, rerun failed workflows, force-push, use mutable `latest` for deployment, or call deployment `force` / `down -v`.
- Do not collapse auxiliary Models or perform unrelated management writes as part of release rollout.
