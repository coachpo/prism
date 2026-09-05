# Endpoint dialogs

- These dialogs render state owned by `../../features/endpoints/`; they do not own Endpoint CRUD or reference caches.
- `DeleteEndpointDialog.tsx` renders fresh preflight eligibility, paged blockers, and lock-time conflict replacement. Unknown/error/stale references cannot enable deletion. `OrphanCleanupDialog.tsx` remains a separate destructive flow for ownerless connections.
- `AttachToModelDialog.tsx` selects a model for one-shot Terminal Target authoring. Navigation carries only `action=create-terminal-target` and `endpoint_id`; key material/fingerprints never enter router state.
