# Endpoint lifecycles

- `useEndpointsFeatureData.ts` composes the list and resource-specific mutation owners. Keep collection/search/filter/sort in `useEndpointList.ts`; create/update/save-and-verify, delete, orphan cleanup, duplication, and attach navigation have separate named hooks.
- `useEndpointReferenceSummaries.ts` owns bounded batch hydration and freshness. `useEndpointReferenceDetails.ts` owns lazy snapshot/cursor detail; `useEndpointReferences.ts` shares their generation fence. A successful detail read atomically replaces that Endpoint's batch summary.
- Unknown/stale reference evidence never becomes zero. Disable reference-derived filters/sorts until summaries are fresh, visibly normalize the filter to `all`, and preserve text search. Cursor/snapshot mismatch restarts detail paging from page one.
- Delete requires a fresh zero-reference preflight. A lock-time `409 endpoint_in_use` replaces the displayed blocker evidence; do not reuse an eligible result after server state moves.
- Save-and-verify commits first, then verifies the committed `config_revision`. A failed verification keeps the saved Endpoint and renders its outcome; stale results must not claim the current configuration was verified.
- Key fields start empty. Replace local DTOs with server responses and use returned key identity timestamps/fingerprint to distinguish unchanged keys from rotation.
- Keep table shell, rows, and reference disclosure in `EndpointTable.tsx`, `EndpointRows.tsx`, and `EndpointReferenceDisclosure.tsx`; retained delete/orphan/attach dialogs live under `../../pages/endpoints/`.

Reference and mutation regressions are covered by `../../test/endpoint-reference-lifecycles.test.tsx` and colocated hook tests.
