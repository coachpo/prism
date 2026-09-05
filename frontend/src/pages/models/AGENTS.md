# Model forms and collection

- `useModelsPageData.ts` composes collection, dialog CRUD, row/bulk enablement, deletion, strategy-default, and metric owners. Keep those lifecycles separate; tables receive state/callbacks.
- `modelFormState.ts` owns model defaults, validation, strategy attachment, direct-entry qualification, and independent OpenAI text/image fields. Non-OpenAI create payloads omit every OpenAI-only key.
- `accessTargetFormState.ts` and `AccessTargetsEditor.tsx` retain one mixed Model Target/Terminal Target list ordered by `(position, id)`. Mutations address persisted access-target row IDs; connection actions address connection IDs. Never use a drag-draft index as persisted identity.
- `useModelsCollection.ts` uses shared reference data and server DTO reconciliation. `modelListProjection.ts` preserves authoritative recursive counts/routing summaries rather than calculating them from direct connections.
- `direct_request_enabled` is the single entry switch. The URL-backed list view exposes direct entries, non-entry Model Targets, or all models, with server incoming-reference counts and warnings. Target configuration still accepts enabled non-entry models.
- Composite create uses `useInitialTerminalTargetUpstreamModelId.ts`: follow the entry ID until the operator edits the upstream ID, then preserve the edit. Reuse `../model-detail/upstreamModelIdField.ts` for validation and server field-error mapping; explicit blank is rejected.
- `useModelMetrics24h.ts` hydrates all three scope blocks in one batch, separately from base collection CRUD. The URL-backed metrics control selects a local block; route-attempt cost remains absent with its reason.
- Keep dialog session fencing when strategy defaults refresh or a modal closes; an old dialog result must not update the current form.
