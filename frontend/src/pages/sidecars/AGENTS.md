# FRONTEND SIDECARS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/sidecars/` owns the global `/sidecars` route behind `../SidecarsPage.tsx`. It presents CLIProxyAPI sidecar registrations, auth/provider inventory, watchdog policy, and action history through Prism's backend-managed sidecar API.

## STRUCTURE
```text
sidecars/
├── SidecarsScaffold.tsx       # Page composition and selected-sidecar detail layout
├── useSidecarsPageData.ts     # Load, poll, CRUD, sync, mutation, and toast orchestration
├── SidecarsTable.tsx          # Sidecar list, health state, test/sync row actions
├── SidecarDialog.tsx          # Create/edit form
├── DeleteSidecarDialog.tsx    # Delete confirmation
├── AuthFilesTable.tsx         # Auth inventory and status/priority mutation UI
├── ProviderInventoryTable.tsx # Provider inventory with masked snapshot summary
├── WatchdogPolicyPanel.tsx    # Watchdog settings form
├── SidecarActionHistory.tsx   # Redacted action history table
└── sidecarFormState.ts        # Form defaults, validation, payload normalization
```

## WHERE TO LOOK

- Thin route shell and page header: `../SidecarsPage.tsx`.
- Route mount and global navigation metadata: `../../App.tsx`, `../../components/layout/app-layout/navigationProfileConfig.ts`.
- Backend client facade: `../../lib/api/sidecars.ts`, exported through `../../lib/api.ts`.
- Payload types: `../../lib/types/sidecar.ts`, re-exported by `../../lib/types.ts`.
- Visible copy and toasts: `../../i18n/messages/en.ts`, `../../i18n/messages/zh-CN.ts` under `sidecarsPage`.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
