# Frontend Rewrite Master Plan

Provenance: this durable docs artifact was summarized from `.omo/plans/frontend-rewrite-master-plan.md` during Task 19 of the frontend rewrite cleanup. The active execution plan remains under `.omo/plans/`; this file preserves the stable rewrite intent, architecture, contract guardrails, and cleanup status for durable reference.

## Summary

Prism's management frontend is being rebuilt as a fresh React, TypeScript, Vite, Tailwind, and shadcn/ui interface. The old frontend was used as a behavioral oracle while the rewrite migrated route ownership into `frontend/src/features/`, routing into `frontend/src/app/router/`, reusable architecture into `frontend/src/shared/`, and shell contracts into the current layout/provider stack.

## Preserved Contracts

- Backend payload semantics, API families, request and response field names, and typed API client ownership remain unchanged.
- Selected-profile management APIs keep centralized `X-Profile-Id` scoping; global sidecar, proxy-key, vendor, auth, and runtime paths remain profile-header-free.
- Auth bootstrap, profile activation/soft-delete, startup confirmations, config import/export preview tokens, vendor catalog import/export, proxy-key reveal/rotation/delete, sidecar stale-auth behavior, loadbalance status transitions, request logs, and audit history remain contract-preserved.
- Public auth routes and protected route mounting remain guarded by the current auth/profile/provider stack.

## Rewrite Architecture

- `frontend/src/app/router/appRouter.tsx` owns the rewrite route tree and compatibility redirects.
- `frontend/src/features/**` owns feature route surfaces such as observe, models, endpoints, ban policies, pricing, settings, proxy keys, sidecars, and request logs.
- `frontend/src/pages/**` now holds public auth pages plus oracle-compatible clusters still imported by feature routes or contract tests.
- `frontend/src/lib/api/**` remains the hand-authored typed backend boundary; generated clients are intentionally not introduced.
- `frontend/src/shared/**` holds rewrite-era reusable contracts for query keys, forms, tables, and design-system helpers.

## Cleanup Status

Task 19 hard-deleted obsolete route shell files and unused helpers only after import graph and text-search checks showed no source or test imports remained. Current rewrite feature modules, shared primitives still in use, API clients, tests, production server files, Vite proxy wiring, fixtures, and public assets were preserved.

Dependencies were reviewed for old router, table, form, drag, chart, and command packages. `vaul` was removed because the generated drawer primitive had no remaining imports. TanStack Router, TanStack Query, TanStack Table, React Hook Form, Zod resolver, React Router compatibility, DnD Kit, Recharts, and `cmdk` remain because current rewrite code or retained oracle-compatible modules still import them.

## Evidence

Task 19 evidence is recorded under `.omo/evidence/frontend-rewrite/`:

- `task-19-deleted-files.txt`
- `task-19-docs-artifact.txt`
- `task-19-build-clean.txt`

Task 20 and the final verification wave own route screenshots, browser preflight, and final multi-agent verification. They are intentionally not performed by Task 19.
