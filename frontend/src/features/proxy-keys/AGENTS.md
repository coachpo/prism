# FRONTEND PROXY KEYS FEATURE

## OVERVIEW
`features/proxy-keys/` owns the `/system/proxy-keys` route orchestration: the ledger/query lifecycle, create/update/rotate/delete mutations, generated-secret session state machine, and the one-time-secret access panel (effective runtime origin, family base URL, model/operation-aware curl, shared runtime self-test entry, saved-acknowledgement gate).

## STRUCTURE
```text
proxy-keys/
├── ProxyKeysFeaturePage.tsx       # Route shell: navigation blocker, beforeunload guard, dialog wiring
├── useProxyKeysFeatureData.ts     # Thin page coordinator for ledger, mutation, and secret-session values
├── useProxyKeyLedger.ts            # Auth/key ledger queries, capacity, visible-key usage, and read retry
├── useProxyKeyMutations.ts         # Thin composition of the four mutation lifecycles
├── useProxyKeyCreateMutation.ts    # Create/issue form validation, mutation, and secret handoff
├── useProxyKeyEditMutation.ts      # Edit form validation, mutation, and server-item patch
├── useProxyKeyRotateMutation.ts    # Rotation confirmation, mutation, and secret handoff
├── useProxyKeyDeleteMutation.ts    # Delete confirmation, mutation, and ledger removal
├── proxyKeyMutationReconciliation.ts # Explicit Proxy-Key ledger cache reconciliation
├── proxyKeyMutationErrors.ts       # Shared Proxy-Key mutation error-to-toast mapping
├── useProxyKeySecretSession.ts     # One-time raw-secret handoff into the guarded session reducer
├── generatedSecretSession.ts       # Unacknowledged session reducer (idle/unacknowledged/closing_confirm)
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
- Keep server-backed ledger/query state in `useProxyKeyLedger.ts`; keep create/issue, edit, rotate, and delete state in their respective named mutation hooks. `useProxyKeyMutations.ts` only composes their return values.
- Keep cache patching in `proxyKeyMutationReconciliation.ts` and error-to-toast mapping in `proxyKeyMutationErrors.ts`; mutation hooks must reuse those owners rather than copying either policy.
- Create and rotate may hand their response to `useProxyKeySecretSession.ts`, but only that session owns the raw key after handoff; no query cache, ledger item, URL, storage, or error path may retain it.
- `ProxyKeysFeaturePage.tsx` owns the standing access-verification dialog's open state; it is not part of any credential mutation lifecycle.
