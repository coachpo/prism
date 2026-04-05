# FRONTEND REQUEST LOGS DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/request-logs/` owns the investigation flow for proxy traffic: retained browse filtering, exact-request focus mode, and detailed payload inspection. This parent also covers the local `detail/` cluster, while request URL-state and exact-request behavior stay local here.

## STRUCTURE
```
request-logs/
├── queryParams.ts               # URL-state contract for retained browse filters and pagination
├── useRequestLogPageState.ts    # Search-param orchestration and exact-request mode
├── useRequestLogsPageData.ts    # Server fetches and retained filter-option bootstrap
├── useAuditDetail.ts            # Lazy audit detail lookup and retry behavior
├── useRequestLogDetail.ts       # Exact-request detail fetch, not-found handling, and refresh
├── columns.tsx                  # Table column definitions and detail entry affordances
├── FiltersBar.tsx               # UI shell for retained browse filters plus refresh/clear actions
├── FiltersBar.constants.ts      # Filter option constants and shared filter presentation helpers
├── FiltersBarPrimaryFilters.tsx # Retained filter row composition
├── RequestLogsTable.tsx         # Paginated and virtualized log list
├── RequestLogDetailSheet.tsx    # Detailed request/audit payload view
├── RequestFocusBanner.tsx       # Exact-request mode banner and exit action
├── connectionNavigation.ts      # Connection-jump helpers for request-log detail context
├── detail/                      # Parent-covered overview, audit, payload, and shared detail helpers
└── __tests__/                   # Query-param, filter, table, and audit-detail coverage
```

## WHERE TO LOOK
- Investigation flow and state, including URL-state and exact-request mode: `useRequestLogsPageData.ts`, `useRequestLogPageState.ts`
- Route-shell copy, empty-state messaging, and locale-aware detail labels: `../RequestLogsPage.tsx`, `@/i18n/useLocale`, `@/i18n/AGENTS.md`
- Retained browse-filter contract and defaults: `queryParams.ts`
- Table columns, row actions, and detail-entry affordances: `columns.tsx`, `RequestLogsTable.tsx`
- Filter-bar composition and shared filter constants: `FiltersBar.constants.ts`, `FiltersBarPrimaryFilters.tsx`, `FiltersBar.tsx`
- Detail sheet, exact-request fetch, and lazy audit fetch: `RequestLogDetailSheet.tsx`, `useRequestLogDetail.ts`, `useAuditDetail.ts`
- Connection navigation helpers for request-log detail context: `connectionNavigation.ts`
- Parent-covered detail cluster helpers: `detail/RequestLogOverviewTab.tsx`, `detail/RequestLogAuditTab.tsx`, `detail/RequestLogPayloadBlock.tsx`, `detail/requestLogDetailShared.tsx`, `detail/requestLogDetailUtils.ts`

## CONVENTIONS
- Treat URL as the source of truth for the retained browse filters to support deep-linking.
- Keep audit payload fetching lazy and isolated from the main request-list fetch lifecycle.
- Use exact-request mode (`request_id`) to switch from paginated browsing to a single-request investigation workflow, and keep that mode local to the request-logs page.
- Keep retained browse filtering on `ingress_request_id`, `model_id`, `endpoint_id`, `status_family`, and `time_range` only.
- Keep user-facing copy on the shared locale boundary through `useLocale()`, while timestamp formatting continues to flow through `useTimezone()`.
- Keep `detail/` parent-covered here. Those helpers support the request-log sheet only and should not get a separate AGENTS file.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not stale-claim that request logs are missing from the route map.
- Do not duplicate filter parsing outside `queryParams.ts`.
- Do not fetch audit payloads during normal table browsing when the detail drawer is closed.
- Do not split `request-logs/detail/` into a separate AGENTS file while this parent already owns that cluster.
