# Management API Guidance

Mount package routes through `MountManagementRoutes` and `../../platform/http/management_branch.go`. That platform branch owns admission, authentication ordering, schema-transition guards, browser-write protection, and mutation-cache effects.

- Resolve profile-scoped resources through `profiledomain.ResolveEffectiveProfile`: the effective profile is Default id `1`, and `X-Profile-Id` does not select another profile. Preserve storage profile columns and predicates; instance-global resources remain global.
- Reuse `responseutil/` for response envelopes, profile errors, and private/no-store headers. Preserve the owning route's typed error contract instead of cloning a new envelope.
- Declare new route admission and runtime-cache effects in the platform route specifications. Catalog metadata writes must remain planning-neutral; routing mutations use the platform invalidation seam after successful writes.
- Keep bootstrap file ownership in `../../platform/config/`; database settings belong in their owning management package.

Follow the local guide for the affected contract:

- [Auth](auth/AGENTS.md): session transitions, proxy keys, and runtime-auth publication.
- [Models](models/AGENTS.md) and [connections](connections/AGENTS.md): graph authoring, private Terminal Targets, pricing, and catalog/export bindings.
- [Endpoints](endpoints/AGENTS.md), [configuration rules](configrules/AGENTS.md), and [load balancing](loadbalance/AGENTS.md): reference-sensitive CRUD and routing state.
- [Stats](stats/AGENTS.md), [audit](audit/AGENTS.md), and [settings](settings/AGENTS.md): retained-history reads, coverage, settings CAS, and job boundaries.
