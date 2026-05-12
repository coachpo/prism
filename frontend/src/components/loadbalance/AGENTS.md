# FRONTEND LOADBALANCE COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`loadbalance/` holds shared renderers for family-aware loadbalance badges, event rows, and the event detail sheet.

## WHERE TO LOOK
- `LoadbalanceBadges.tsx`
- `LoadbalanceEventDetailSheet.tsx`
- `LoadbalanceEventsTable.tsx`

## CONVENTIONS

- When doing upgrade work, first account for this project stage: This application is under development, it doesn't have users at the moment. Backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested; prefer the best current implementation shape over preserving the old one, and do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.
- Keep these components presentational and feed them shaped props.
- Keep route-specific data loading out of this folder.

## ANTI-PATTERNS
- Do not move route-state or realtime orchestration into this shared folder.
- Do not duplicate page-local event formatting when the shared detail sheet or table already owns the presentation.
