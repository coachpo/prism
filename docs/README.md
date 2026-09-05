# Documentation Index

The canonical Prism documentation is maintained in English. Each fact or policy has one authority; specialized documents retain their distinct purpose.

## Canonical Documents

| Document | Authority |
| --- | --- |
| [README.md](../README.md) | Project entry, installation, ordinary startup, and a derived status summary. |
| [STATUS.md](../STATUS.md) | Required development tier, lifecycle, deployments, users, retained data, compatibility, and allowed/prohibited changes. |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | Development setup, commands, workflow, the static strategy selected by STATUS, shared principles, and definition of done. The strategy is an execution default, not a source of deployment or data facts. |
| [product.md](product.md) | Product scope, users, flows, requirements, acceptance, Requests specification (§8), and workflows (§9). |
| [architecture.md](architecture.md) | Current components, dependencies, data lifecycle, runtime boundaries, API reference (§14), data model (§15), and engineering invariants (§16). |
| [development-rules.md](development-rules.md) | Project-specific technical, implementation, and test ownership rules. |
| [source-code-size-and-responsibility-rules.md](source-code-size-and-responsibility-rules.md) | Shared source responsibility and size policy, rendered from the documentation skill's asset. |

## Specialized Documents

- [Frontend design system](../frontend/DESIGN.md): UI/UX authority, semantic tokens, operator components, density, accessibility, and honest evidence presentation.
- [Direct-entry reclassification runbook](operations/direct-request-entry-reclassification.md): the operator-invoked 12-entry/four-mapping procedure, hash-bound to [versioned SQL](../scripts/operations/direct-request-entry-reclassification.sql) and its [disposable acceptance test](../backend/tests/integration/direct_request_entry_reclassification_plan_test.go).
- [Prism operations inspection](../.agents/skills/prism-ops-inspect/SKILL.md): read-only repository and deployment snapshot procedure with the deployment adapter and evidence contract.
- [Prism backup and restore](../.agents/skills/prism-backup-restore/SKILL.md): backup creation, validation, retention, and recovery procedures.
- [Prism release and deploy](../.agents/skills/prism-release-deploy/SKILL.md): explicitly authorized release and staged rollout procedure, with immutable image and recovery evidence.

These operator skills retain their own references and scripts; they are executable workflows, not competing product or architecture documents. Active plans and observation artifacts belong under ignored `artifacts/plans/` and `artifacts/evidence/`.

## Agent Guidance

[Root AGENTS.md](../AGENTS.md) routes engineering work to the [backend](../backend/AGENTS.md), [frontend](../frontend/AGENTS.md), and [documentation](AGENTS.md) guidance. Nested guides contain local responsibilities and constraints; canonical documents own the shared project facts.
