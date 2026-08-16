# FRONTEND PROXY KEYS FEATURE

## OVERVIEW
`features/proxy-keys/` owns the `/system/proxy-keys` route orchestration: the generated-secret session state machine, ledger/mutation reconciliation, and the one-time-secret access panel (effective runtime origin, family base URL, model/operation-aware curl, shared runtime self-test entry, saved-acknowledgement gate).

## STRUCTURE
```text
proxy-keys/
├── ProxyKeysFeaturePage.tsx       # Route shell: navigation blocker, beforeunload guard, dialog wiring
├── useProxyKeysFeatureData.ts     # Ledger query, mutations, capacity reconciliation, session dispatch
├── generatedSecretSession.ts      # Mutation-owned unacknowledged session reducer (idle/unacknowledged/closing_confirm)
├── ProxyKeySecretDialog.tsx       # One-time-secret access panel with ack gate and closing-confirm state
└── useProxyKeyUsage.ts            # Per-key 7-day counts for the ledger column, with its own failure state
```
The shared runtime self-test is not here; it lives in `../runtime-self-test/`.

## CONVENTIONS
- The raw key exists only in the create/rotate response and the unacknowledged in-memory session; it never enters query cache, URL, storage, logs, or error reporting. Mutation-owned response data is reset after dispatch.
- The session reducer is immune to list/auth/model/stats refetch events; query failures never close the dialog.
- Escape, mask clicks, close, SPA navigation and refresh are blocked while unacknowledged; `useBlocker` + `beforeunload` cover navigation and refresh; closing-confirm offers keep-editing or explicit abandon.
- Copy actions never auto-acknowledge; finish is enabled only after the saved acknowledgement.
- Create/rotate fetches use `cache: "no-store"`; the backend responses carry `Cache-Control: private, no-store`.
