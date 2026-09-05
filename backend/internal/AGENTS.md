# Backend Internal Ownership

Choose the owning boundary before editing:

- [Platform](platform/AGENTS.md): process composition, bootstrap, HTTP assembly, database lanes, migrations, and background work.
- [HTTP API](httpapi/AGENTS.md): request parsing, authentication, management scope, runtime ingress, and response shaping.
- [Gateway](gateway/AGENTS.md): provider-neutral envelopes, native adapters, route planning, reservations, and accounting.
- [Domain](domain/AGENTS.md): HTTP-neutral routing state, catalog clients, export, retained read models, and diagnostics.

`pgxutil/tx.go` owns shared transaction mechanics, not feature policy. Keep `endpointdomain/`, `profiledomain/`, and `providerauth/` narrow shared contracts. `openaimodecheck/` is the read-only native text-mode equality check shared by startup and upgrade preflight.

Do not move HTTP parsing or profile resolution into domain packages, provider behavior into platform assembly, or feature policy into shared transaction helpers.
