# Documentation Guide

## Ownership

- [README.md](../README.md#documentation) routes the canonical document set. [ARCHITECTURE.md](../ARCHITECTURE.md) owns component boundaries, the tech stack, and routes the API, data-model, and topic references.
- [../CONTRIBUTING.md](../CONTRIBUTING.md) owns commands, development workflow, product scope, the retained-data and compatibility policy, the selected tier strategy, the source-size rules, and shared principles. [Project Engineering Rules](../CONTRIBUTING.md#project-engineering-rules) and [Test Ownership](../CONTRIBUTING.md#test-ownership) own project-specific technical and test rules.
- [operations.md](operations.md) owns deployment facts, the deployed-instance record, runbooks, and recovery. Keep implementation detail and release narratives out of it.
- [../frontend/DESIGN.md](../frontend/DESIGN.md) remains the UI/UX authority. Link it rather than duplicating its tokens and component rules.

## Evidence and Placement

- Keep durable facts tied to current source, migrations, tests, and CI. Use the [API contract references](../ARCHITECTURE.md#api-contracts) and the [data model](data-model.md). Do not recreate competing standalone references.
- One topic per file under `docs/`, named in lowercase with hyphens, and linked from `ARCHITECTURE.md`. Section headings carry no inherited numbering.
- [Runtime API routing failures](runtime-api.md#routing-failures) own routing-schedule planning codes and wire fields; behavior prose describes behavior without maintaining a second field list.
- Maintained operator runbooks live inside [operations.md](operations.md) with executable companions under `../scripts/operations/`. The [direct-entry runbook](operations.md#direct-request-entry-reclassification) is hash-bound to its SQL by `../backend/tests/integration/direct_request_entry_reclassification_plan_test.go`; preserve its SHA-256 line, identity lists, edge lines, and path strings, and keep the shipped-bundle statement in `CONTRIBUTING.md` that the test reads. A runbook documents the reusable procedure; instance observations and execution results do not become permanent runbook claims.
- When merging a document, retain its unique current contracts in the appropriate authority and fix affected links before deleting it.
