---
name: prism-release-deploy
description: Release, package, and deploy Prism through release.sh, GitHub CI, immutable GHCR image manifests, verified backups, and staged Compose rollouts with migration, health, smoke, observation, and stop gates. Use only for explicitly authorized Prism release or deployment work; use prism-ops-inspect for diagnosis-only requests.
metadata:
  short-description: Release and deploy Prism with staged gates
---

# Prism Release Deploy

## Outcome

Publish an immutable Prism release manifest, then consume it in a separately authorized A-before-B rollout. Complete only after every configured backup, migration, health, smoke, observation, and retention gate passes.

## Authorization

- `plan` is read-only and may inspect repository, GitHub, registry, and deployment state.
- Release execution requires current release/tag/push/package authorization plus `--confirm-release vX.Y.Z`.
- Rollout requires current deployment authorization plus `--confirm-rollout <tag>@<release-sha-prefix>`. Provider traffic additionally requires `--allow-provider-smoke`; prune requires one exact confirmation per service.
- Confirmation flags record authorization but never create it. Do not reuse authorization from another task or stage.

## Release stage

1. Run `scripts/prism_release.py plan --spec <patch|minor|major|X.Y.Z>`.
2. Before execution, read [references/release-manifest.md](references/release-manifest.md).
3. Run the repository `release.sh`; wait for the release commit's CI and tag-triggered Docker Image workflow, not the pre-release commit's CI.
4. Verify repository, tag, release SHA, OCI revision/version/platform, and full manifest digest. Write `status: published` evidence under `artifacts/evidence/prism-ops/releases/` only after all gates pass.
5. If the release commit, tag, workflows, and image already published but manifest creation failed, use `prism_release.py recover --spec X.Y.Z --confirm-release vX.Y.Z`. Recovery is validation-only: it requires a clean main containing the release tag, identical local/remote tag identity, green release workflows, and matching OCI evidence; it never reruns `release.sh`, pushes, tags, or rebuilds.

## Rollout stage

1. Run `scripts/prism_rollout.py plan --manifest <published.json> ...`.
2. Read [references/rollout-gates.md](references/rollout-gates.md). For `capy`, also read [../prism-ops-inspect/references/capy.md](../prism-ops-inspect/references/capy.md).
3. Create a verified backup through `$prism-backup-restore`; deploy A before B, and leave B untouched until A passes every gate.
4. On failure after schema advancement, stop the affected app and preserve its database and backup for an explicit restore decision.

## Completion

- Never create a GitHub Release, rerun failed workflows, force-push, deploy `latest`, use `force` / `down -v`, collapse auxiliary Models, or perform unrelated management writes.
- Lead the handoff with release SHA/tag, CI and image evidence, immutable image reference, per-service backup and migration results, smoke/observation results, stopped or unverified states, and required operator action.
