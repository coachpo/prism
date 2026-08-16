# FRONTEND PROXY API KEYS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/proxy-api-keys/` keeps proxy-key widgets still imported by the feature-owned `/system/proxy-keys` route under `src/features/proxy-keys/`. Runtime proxy API keys are global instance credentials, not Default-profile management state.

## STRUCTURE
```text
proxy-api-keys/
├── ProxyKeyDeleteAlertDialog.tsx # Destructive delete confirmation and delete-impact warnings
├── ProxyKeyDetailSheet.tsx       # Sheet-based metadata, notes, expiry, and active-state edit flow
├── ProxyKeyEnforcementPanel.tsx  # Single full-width enforcement status bar with the Settings CTA
├── ProxyKeyExpiryField.tsx       # Settings-timezone expiry field with DST gap/overlap handling and preserve/set/clear tri-state
├── ProxyKeyIssuePanel.tsx        # Issuance drawer opened from the page header
├── ProxyKeyLedgerCard.tsx        # Issued-key ledger: capacity summary, sortable/paginated columns, 7d usage column, row actions
├── ProxyKeyRotateAlertDialog.tsx # Rotation confirmation with the lifecycle consequences
├── ProxyKeyVerifyAccessDialog.tsx# Standing self-test entry: pasted key plus model selection
└── proxyKeyFormatting.ts         # Auth-status tier, lifecycle tier, date, and quota helpers
```

## WHERE TO LOOK
- Feature route and proxy-key data orchestration: `../../features/proxy-keys/`
- Auth enforcement and quota display: `ProxyKeyEnforcementPanel.tsx`, `proxyKeyFormatting.ts`
- Create, edit, rotate, delete, and ledger presentation: `ProxyKeyIssuePanel.tsx`, `ProxyKeyDetailSheet.tsx`, `ProxyKeyRotateAlertDialog.tsx`, `ProxyKeyDeleteAlertDialog.tsx`, `ProxyKeyLedgerCard.tsx`
- 7-day usage counts for the ledger column: `../../features/proxy-keys/useProxyKeyUsage.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Treat proxy API key management as a global auth-settings workflow, not a Default-profile feature.
- Bootstrap auth settings and existing keys in parallel with `Promise.allSettled()` in the feature data hook.
- Patch the local key list after create, edit, rotate, and delete flows instead of reloading the whole page; reconcile capacity from the server snapshot returned by each mutation.
- Rotation and delete both require an explicit confirmation dialog; neither may fire straight from a row control.
- The ledger's 7-day usage column owns its own read: a failed count renders as a failure with a staleness badge on the table, never as `0` and never as a blank cell. It is deliberately not sortable, because its query set is the visible page.
- The one-time secret session lives in `src/features/proxy-keys/generatedSecretSession.ts` and is mutation-owned: list/auth/model/stats refetches never close it; Escape/mask/close/navigation/refresh are blocked until the operator acknowledges the key is saved; after finish/abandon all raw-key references are released.
- Expiry is edited in the Settings timezone and submitted as RFC3339; DST gap times are blocked and overlaps resolve to the earlier occurrence with a notice.
- Do not scope proxy-key UX to Default-profile management state; runtime keys are global instance credentials.
