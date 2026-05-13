# FRONTEND LOADBALANCE COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`loadbalance/` holds shared renderers for family-aware loadbalance badges, event rows, and the event detail sheet.

## WHERE TO LOOK
- `LoadbalanceBadges.tsx`
- `LoadbalanceEventDetailSheet.tsx`
- `LoadbalanceEventsTable.tsx`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Keep these components presentational and feed them shaped props.
- Keep route-specific data loading out of this folder.

## ANTI-PATTERNS
- Do not move route-state or realtime orchestration into this shared folder.
- Do not duplicate page-local event formatting when the shared detail sheet or table already owns the presentation.
