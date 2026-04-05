# FRONTEND LOADBALANCE COMPONENTS KNOWLEDGE BASE

## OVERVIEW
`loadbalance/` holds shared renderers for family-aware loadbalance badges, event rows, and the event detail sheet.

## WHERE TO LOOK
- `LoadbalanceBadges.tsx`
- `LoadbalanceEventDetailSheet.tsx`
- `LoadbalanceEventsTable.tsx`
- `__tests__/`

## CONVENTIONS
- Keep these components presentational and feed them shaped props.
- Keep route-specific data loading out of this folder.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not move route-state or realtime orchestration into this shared folder.
- Do not duplicate page-local event formatting when the shared detail sheet or table already owns the presentation.
