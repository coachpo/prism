# Documentation Guide

## Ownership

- [README.md](README.md) routes the canonical document set. Product behavior belongs in [product.md](product.md); components, interfaces, and schemas belong in [architecture.md](architecture.md).
- [../STATUS.md](../STATUS.md) owns lifecycle, deployment, retained-data, and compatibility facts. Keep implementation detail and release narratives out of status; link to the owning architecture or product section.
- [../CONTRIBUTING.md](../CONTRIBUTING.md) owns commands, development workflow, selected tier strategy, and shared principles. [development-rules.md](development-rules.md) owns project-specific technical and test rules.
- Preserve the `write-project-docs` managed blocks with their bundled updaters. [source-code-size-and-responsibility-rules.md](source-code-size-and-responsibility-rules.md) is rendered from the shared asset and must not gain project-specific additions.
- [../frontend/DESIGN.md](../frontend/DESIGN.md) remains the UI/UX authority. Link it rather than duplicating its tokens and component rules.

## Evidence and Placement

- Keep durable facts tied to current source, migrations, tests, and CI. Architecture sections 14 and 15 own the API and data-model references; product sections 8 and 9 own Requests and workflows. Do not recreate competing standalone references.
- Architecture §14.2.2A owns routing-schedule planning codes and wire fields; product prose describes behavior without maintaining a second field list.
- `operations/` holds maintained operator runbooks with executable companions under `../scripts/operations/`. The direct-entry runbook is hash-bound to its SQL by `../backend/tests/integration/direct_request_entry_reclassification_plan_test.go`; preserve that source binding.
- Put active plans in `../artifacts/plans/` and execution evidence in `../artifacts/evidence/`. A runbook documents the reusable procedure; instance observations and execution results do not become permanent runbook claims.
- When merging a document, retain its unique current contracts in the appropriate authority and fix affected links before deleting it. Preserve independent design/runbook value and other managed regions.
