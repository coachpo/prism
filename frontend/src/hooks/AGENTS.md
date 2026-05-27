# FRONTEND HOOKS KNOWLEDGE BASE

## OVERVIEW
`src/hooks/` contains Prism's shared reactive helpers for realtime updates, periodic polling, and shared display formatting.

## STRUCTURE
```
hooks/
├── useRealtimeData.ts   # Shared realtime subscription hook over the singleton websocket client
├── usePolling.ts        # Tab-visibility-aware polling hook
└── useTimezone.ts       # Shared timestamp formatting hook over i18n helpers
```

## WHERE TO LOOK

- Shared realtime subscription lifecycle, profile-aware channel wiring, and preferred consumer path over the singleton websocket client: `useRealtimeData.ts`, `../lib/websocket.ts`
- Standard periodic refresh with visibility-aware start/stop behavior: `usePolling.ts`
- Shared timestamp formatting through the locale layer: `useTimezone.ts`, `../i18n/format.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Prefer `useRealtimeData()` over direct WebSocket access, including when a page needs shared connection state from the singleton client.
- Use `usePolling()` for observability pages that do not have full realtime coverage.
- Keep hook side effects small, and push complex shaping into `src/lib/` or local page helpers.
- Route shared date and time display through `useTimezone.ts` or `src/i18n/format.ts`.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not create ad hoc `setInterval` loops in components.
- Do not duplicate reconnect buffering or websocket state handling outside `useRealtimeData.ts` and `src/lib/websocket.ts`.
- Do not duplicate timezone formatting logic outside `useTimezone.ts` and `src/i18n/format.ts`.
