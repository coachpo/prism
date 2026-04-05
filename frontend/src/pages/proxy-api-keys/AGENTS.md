# FRONTEND PROXY API KEYS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/proxy-api-keys/` owns global proxy-key issuance, edit, rotation, deletion, auth-status messaging, and one-time secret display behind `../ProxyApiKeysPage.tsx`. These are instance credentials, so the page stays global rather than selected-profile scoped.

## STRUCTURE
```
proxy-api-keys/
├── ProxyKeyCreateCard.tsx        # Create form and latest-generated-key display
├── ProxyKeysListCard.tsx         # Existing key list with actions
├── ProxyKeyCard.tsx              # Individual key row/card presentation
├── ProxyKeyStatusCallout.tsx     # Auth-enabled guidance and warnings
├── EditProxyKeyDialog.tsx        # Edit name, note, and active-state flow
├── DeleteProxyKeyDialog.tsx      # Delete confirmation flow
├── ProxyApiKeysPageSkeleton.tsx  # Loading state
├── proxyKeyFormatting.ts         # Auth-status tone helpers
└── useProxyApiKeysPageData.ts    # Parallel bootstrap, create, edit, rotate, delete, and badge state
```

## WHERE TO LOOK

- Parallel bootstrap of auth settings and current keys: `useProxyApiKeysPageData.ts`
- Create and rotate flows with one-time secret handling: `useProxyApiKeysPageData.ts`, `ProxyKeyCreateCard.tsx`
- Edit dialog flow for stored metadata and active state: `EditProxyKeyDialog.tsx`, `ProxyKeysListCard.tsx`, `useProxyApiKeysPageData.ts`
- Delete confirmations and list patching: `DeleteProxyKeyDialog.tsx`, `useProxyApiKeysPageData.ts`
- Auth-status badge tone and callout copy: `proxyKeyFormatting.ts`, `ProxyKeyStatusCallout.tsx`

## CONVENTIONS

- Treat proxy API key management as a global auth-settings workflow, not a selected-profile feature.
- Bootstrap auth settings and existing keys in parallel with `Promise.allSettled()`.
- Patch the local key list after create, edit, rotate, and delete flows instead of reloading the whole page.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not scope proxy-key UX to the selected profile; runtime keys are global instance credentials.
- Do not discard the latest generated secret before the user has a chance to copy it.
- Do not reload the page after create, edit, rotate, or delete when `useProxyApiKeysPageData.ts` already patches state locally.
