# Release manifest

## Acceptance and source identity

Before publishing, record the accepted feature/integration SHA and applicable test evidence. After `release.sh`, compare that source with the release commit: accepted product files must still match; the expected release-only changes are `VERSION`, `backend/VERSION`, `frontend/VERSION`, and the version field in `frontend/package.json`. If product source changed, validate the affected changes before treating earlier acceptance as release evidence. Save this comparison separately, for example as `release-source-bridge.json`; it is not a new required field in the manifest schema.

The v1.1.11 performance release and v1.1.12 OpenCode release used this sequence: acceptance, authorized integration on main, release helper, release-SHA CI, tag image workflow, immutable image verification, then deployment. Feature-specific tests and migration expectations differ per release. A later `STATUS.md` commit does not replace the release SHA or require retagging.

## Published artifact

`prism_release.py execute` writes schema version 1 JSON only after publishing succeeds.

Required fields:

- `status: published`, `created_at`, `repository`, `release_spec`, `version`, `tag`, and full `release_sha`;
- release commit subject and four aligned version surfaces;
- CI and Docker workflow IDs, URLs, status, and conclusion;
- `image.repository`, immutable `image.ref` (`tag@sha256:digest`), manifest digest, OS, architecture, OCI revision, and OCI version.

The script resolves the GitHub repository from `origin`, uses `GITHUB_TOKEN` only when already present, never prints it, and otherwise uses the public API. A failure writes no published manifest. If `release.sh` leaves a partial local state, stop and report it rather than automatically reverting or creating another tag.

The manifest is immutable input to rollout. Rollout binds the top-level GitHub repository to `image.repository`, the release tag to the exact image tag, and a full 64-hex SHA-256 digest to the immutable reference. It refuses a non-published manifest, any identity mismatch, mutable-only tags, or OCI metadata drift.

When publishing succeeded but post-publish verification or manifest writing failed, `prism_release.py recover --spec X.Y.Z --confirm-release vX.Y.Z` reconstructs the same schema only from existing facts. It requires clean/current `main`, a local and remote release tag with the same commit, the tag commit to remain in main history, aligned version surfaces, green release-SHA CI and tag Docker workflows, and matching OCI digest/revision/version/platform. Recovery performs no Git or registry mutation and refuses to overwrite an existing manifest.
