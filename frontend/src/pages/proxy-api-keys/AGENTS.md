# FRONTEND PROXY API KEYS COMPATIBILITY CLUSTER

## OVERVIEW
`pages/proxy-api-keys/` keeps proxy-key widgets still imported by the feature-owned `/control/proxy-keys` route under `src/features/proxy-keys/`. Runtime proxy API keys are global instance credentials, not selected-profile state.

## STRUCTURE
```text
proxy-api-keys/
├── ProxyApiKeysPageSkeleton.tsx  # Key Vault Console loading structure
├── ProxyKeyDeleteAlertDialog.tsx # Destructive delete confirmation and delete-impact warnings
├── ProxyKeyDetailSheet.tsx       # Sheet-based metadata, notes, expiry, and active-state edit flow
├── ProxyKeyEnforcementPanel.tsx  # Auth enforcement state and proxy-key quota rail
├── ProxyKeyIssuePanel.tsx        # Field-based credential issuance form
├── ProxyKeyLedgerCard.tsx        # Issued-key ledger, lifecycle labels, lineage, and row actions
└── proxyKeyFormatting.ts         # Auth-status tone, lifecycle, date, and quota helpers
```

## WHERE TO LOOK
- Feature route and proxy-key data orchestration: `../../features/proxy-keys/`
- Auth enforcement and quota display: `ProxyKeyEnforcementPanel.tsx`, `proxyKeyFormatting.ts`
- Create, edit, rotate, delete, and ledger presentation: `ProxyKeyIssuePanel.tsx`, `ProxyKeyDetailSheet.tsx`, `ProxyKeyDeleteAlertDialog.tsx`, `ProxyKeyLedgerCard.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid deprecated compatibility wrappers listed there.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Treat proxy API key management as a global auth-settings workflow, not a selected-profile feature.
- Bootstrap auth settings and existing keys in parallel with `Promise.allSettled()` in the feature data hook.
- Patch the local key list after create, edit, rotate, and delete flows instead of reloading the whole page.
- Do not scope proxy-key UX to the selected profile; runtime keys are global instance credentials.
