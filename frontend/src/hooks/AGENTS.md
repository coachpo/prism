# FRONTEND HOOKS KNOWLEDGE BASE

## OVERVIEW
`src/hooks/` contains Prism's shared reactive helpers for realtime updates, periodic polling, and shared display formatting.

## STRUCTURE
```
hooks/
├── useRealtimeData.ts   # Shared realtime subscription hook over the singleton websocket client
├── usePolling.ts        # Tab-visibility-aware polling hook
├── useTimezone.ts       # Shared timestamp formatting hook over i18n helpers
└── __tests__/           # Hook-level regression coverage
```

## WHERE TO LOOK

- Shared realtime subscription lifecycle and profile-aware channel wiring: `useRealtimeData.ts`, `../lib/websocket.ts`
- Standard periodic refresh with visibility-aware start/stop behavior: `usePolling.ts`
- Shared timestamp formatting through the locale layer: `useTimezone.ts`, `../i18n/format.ts`

## CONVENTIONS

- Prefer `useRealtimeData()` over direct WebSocket access.
- Use `usePolling()` for observability pages that do not have full realtime coverage.
- Keep hook side effects small, and push complex shaping into `src/lib/` or local page helpers.
- Route shared date and time display through `useTimezone.ts` or `src/i18n/format.ts`.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not create ad hoc `setInterval` loops in components.
- Do not duplicate reconnect buffering or websocket state handling outside `useRealtimeData.ts` and `src/lib/websocket.ts`.
- Do not duplicate timezone formatting logic outside `useTimezone.ts` and `src/i18n/format.ts`.
