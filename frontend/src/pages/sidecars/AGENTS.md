# FRONTEND SIDECARS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/sidecars/` owns the global `/sidecars` route behind `../SidecarsPage.tsx`. It presents CLIProxyAPI sidecar registrations, live auth-files, and provider inventory through Prism's backend-managed sidecar API.

## STRUCTURE
```text
sidecars/
├── SidecarsScaffold.tsx       # Page composition and selected-sidecar detail layout
├── useSidecarsPageData.ts     # Load, poll, CRUD, sync, mutation, and toast orchestration
├── SidecarsTable.tsx          # Sidecar list, health state, test/sync row actions
├── SidecarDialog.tsx          # Create/edit form
├── DeleteSidecarDialog.tsx    # Delete confirmation
├── AuthFilesTable.tsx         # Auth inventory and status/priority mutation UI
├── ProviderInventoryTable.tsx # Provider inventory with masked observation summary
└── sidecarFormState.ts        # Form defaults, validation, payload normalization
```

## WHERE TO LOOK

- Thin route shell and page header: `../SidecarsPage.tsx`.
- Page composition and retained detail panels: `SidecarsScaffold.tsx`, `AuthFilesTable.tsx`, `ProviderInventoryTable.tsx`.
- Load, poll, CRUD, sync, and mutation orchestration: `useSidecarsPageData.ts`.
- Route mount and global navigation metadata: `../../App.tsx`, `../../components/layout/app-layout/navigationProfileConfig.ts`.
- Backend client facade: `../../lib/api/sidecars.ts`, exported through `../../lib/api.ts`.
- Payload types: `../../lib/types/sidecar.ts`, re-exported by `../../lib/types.ts`.
- Visible copy and toasts: `../../i18n/messages/en.ts`, `../../i18n/messages/zh-CN.ts` under `sidecarsPage`.
- Browser regression coverage: `../../../tests/e2e/sidecars.spec.ts`.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing sidecar surface is itself a shipped control-plane contract or guardrail.
- Keep the route global; selected profile state must not scope sidecar API calls.
- Keep the browser on Prism's typed sidecar API client. It must not call CLIProxyAPI directly.
- Keep visible detail panels limited to sidecar metadata, auth-file inventory, read-only auth-file model discovery, provider inventory, direct auth-file status or priority edits, test connection, and manual sync.
- Keep management password values write-only. Forms may submit new values, but list and detail surfaces should render only credential metadata.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).
