# FRONTEND PROXY API KEYS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/proxy-api-keys/` owns global proxy-key issuance, edit, rotation, deletion, auth-status messaging, and one-time secret display behind `../ProxyApiKeysPage.tsx`. These are instance credentials, so the page stays global rather than selected-profile scoped.

## STRUCTURE
```
proxy-api-keys/
├── ProxyKeyDeleteAlertDialog.tsx # Destructive delete confirmation and delete-impact warnings
├── ProxyKeyDetailSheet.tsx       # Sheet-based metadata, notes, expiry, and active-state edit flow
├── ProxyKeyEnforcementPanel.tsx  # Auth enforcement state and proxy-key quota rail
├── ProxyKeyIssuePanel.tsx        # Field-based credential issuance form
├── ProxyKeyLedgerCard.tsx        # Issued-key ledger, lifecycle labels, lineage, and row actions
├── ProxyKeySecretReveal.tsx      # One-time create/rotate secret reveal and copy action
├── ProxyApiKeysPageSkeleton.tsx  # Key Vault Console loading structure
├── proxyKeyFormatting.ts         # Auth-status tone, lifecycle, date, and quota helpers
└── useProxyApiKeysPageData.ts    # Parallel bootstrap, create, edit, rotate, delete, and badge state
```

## WHERE TO LOOK

- Parallel bootstrap of auth settings and current keys: `useProxyApiKeysPageData.ts`
- Key Vault Console composition and responsive zone order: `../ProxyApiKeysPage.tsx`
- Create and rotate flows with one-time secret handling: `useProxyApiKeysPageData.ts`, `ProxyKeyIssuePanel.tsx`, `ProxyKeySecretReveal.tsx`
- Auth enforcement and quota display: `ProxyKeyEnforcementPanel.tsx`, `proxyKeyFormatting.ts`
- Edit flow for stored metadata, notes, expiry, and active state: `ProxyKeyDetailSheet.tsx`, `useProxyApiKeysPageData.ts`
- Delete confirmations, auth-enabled warning, successor warning, and list patching: `ProxyKeyDeleteAlertDialog.tsx`, `useProxyApiKeysPageData.ts`
- Auth-status badge tone, ledger lifecycle copy, and lineage labels: `proxyKeyFormatting.ts`, `ProxyKeyLedgerCard.tsx`

## CONVENTIONS

- Treat proxy API key management as a global auth-settings workflow, not a selected-profile feature.
- Bootstrap auth settings and existing keys in parallel with `Promise.allSettled()`.
- Patch the local key list after create, edit, rotate, and delete flows instead of reloading the whole page.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not scope proxy-key UX to the selected profile; runtime keys are global instance credentials.
- Do not discard the latest generated secret before the user has a chance to copy it.
- Do not reload the page after create, edit, rotate, or delete when `useProxyApiKeysPageData.ts` already patches state locally.
