# Documentation Index

Routing page for the Prism documentation set, maintained in English.

## Canonical Documents and Authority

| Path | Authoritative scope |
| --- | --- |
| [README.md](../README.md) | Project entry, installation, ordinary startup, derived status summary, and links. |
| [STATUS.md](../STATUS.md) | Lifecycle, deployment, users, data, and compatibility policy; the authoritative status facts. |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | Development environment, local startup, tests, checks, builds, development workflow, and the shared design and implementation principles plus the Definition of Done. |
| [product.md](product.md) | Product problems, target users, goals, scope, flows, requirements, and acceptance facts (includes the merged requests-page specification and workflows reference). |
| [architecture.md](architecture.md) | Current architecture, components and responsibilities, data lifecycle, dependency direction, interfaces, security boundaries, local run model, quality attributes, risks, and the merged API reference and data model reference. |
| [development-rules.md](development-rules.md) | Project-specific code style, review, and technical implementation rules; links the shared rules in `CONTRIBUTING.md`. |
| [source-code-size-and-responsibility-rules.md](source-code-size-and-responsibility-rules.md) | The unified, project-agnostic source size and responsibility policy; a standalone authoritative document. |

## How the Pieces Fit

- `CONTRIBUTING.md` is the entry point for development workflow, shared principles, and the Definition of Done.
- `development-rules.md` is the authority for project- and technology-specific implementation rules.
- `source-code-size-and-responsibility-rules.md` is the standalone size and responsibility policy; it is linked, not copied.
- The architecture document carries the merged API reference (section 14) and data model reference (section 15); the product document carries the merged requests-page specification (section 8) and workflows reference (section 9).

## Instruction Files

- [AGENTS.md](AGENTS.md) is the docs-directory instruction file: it owns the doc ownership map and the local artifact handoff rules (`../artifacts/plans/`, `../artifacts/evidence/`).
