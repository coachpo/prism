# Management API Guidance

Mount package routes through `MountManagementRoutes` and `../../platform/http/management_branch.go`. That platform branch owns admission, authentication ordering, schema-transition guards, browser-write protection, and mutation-cache effects.

- Resolve profile-scoped resources through `profiledomain.ResolveEffectiveProfile`. Preserve storage profile columns and predicates; instance-global resources remain global.
- Reuse `responseutil/` for response envelopes, profile errors, and private/no-store headers. Preserve the owning route's typed error contract instead of cloning a new envelope.
- Declare new route admission and runtime-cache effects in the platform route specifications. Catalog metadata writes must remain planning-neutral; routing mutations use the platform invalidation seam after successful writes.
- Database settings belong in their owning management package.
