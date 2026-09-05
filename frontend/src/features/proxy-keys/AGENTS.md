# Proxy-key lifecycles

- Keep ledger/auth/capacity reads in `useProxyKeyLedger.ts`; create, edit, rotate, and delete have separate mutation hooks. Their cache reconciliation and error mapping belong to `proxyKeyMutationReconciliation.ts` and `proxyKeyMutationErrors.ts`.
- Create/rotate responses use `no-store` and hand raw keys to `useProxyKeySecretSession.ts`/`generatedSecretSession.ts`; reset mutation-owned response data after handoff. The raw value never enters ledger/query cache, URL, persistent storage, logs, or error reporting.
- Query refetches, failures, and list membership never close an unacknowledged secret session. Copy is not a saved acknowledgement. Finish requires acknowledgement; close/Escape/mask/navigation/refresh remain guarded with explicit abandon as the alternative.
- `ProxyKeysFeaturePage.tsx` owns router/beforeunload guards and the standing verification dialog. It composes the named lifecycle owners instead of making verification part of a credential mutation.
- Self-test/access selectors use enabled direct-entry models. The shared runner under `../runtime-self-test/` owns origin/curl/runtime calls.
- `useProxyKeyUsage.ts` has independent visible-page usage state; a failed read must not manufacture zero usage or close a secret dialog.

Use the colocated mutation/session/usage tests and `../../test/proxy-key-secret-dialog.test.tsx` for this boundary.
