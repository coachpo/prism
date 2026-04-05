# FRONTEND WEBSOCKET HELPER KNOWLEDGE BASE

## OVERVIEW
`src/lib/websocket/` owns the helper split behind `../websocket.ts`: realtime message builders and parsing, ref-counted channel subscription bookkeeping, and transport or reconnect calculations for `/api/realtime/ws`.

## STRUCTURE
```
websocket/
├── protocol.ts      # Message builders, JSON parsing, and heartbeat -> pong policy
├── subscriptions.ts # Ref-counted subscribe/unsubscribe bookkeeping by channel
└── transport.ts     # URL construction, initial connection state, and reconnect delay math
```

## WHERE TO LOOK
- Singleton socket lifecycle, profile switching, heartbeat timers, and reconnect loop: `../websocket.ts`
- Raw message builders, parsing, and heartbeat reply rules: `protocol.ts`
- Channel ref-count increment or decrement behavior: `subscriptions.ts`
- `/api/realtime/ws` URL derivation, initial connection state, and reconnect delay math: `transport.ts`

## CONVENTIONS
- Keep raw websocket message builders and parsing in `protocol.ts`.
- Keep channel subscription ref-count math in `subscriptions.ts`; `../websocket.ts` consumes the helpers but should not duplicate the logic.
- Keep URL construction and reconnect timing policy in `transport.ts`, while `../websocket.ts` owns the actual socket lifecycle and event handlers.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS
- Do not parse raw realtime JSON or hand-build subscribe or unsubscribe payloads outside `protocol.ts`.
- Do not duplicate ref-counted subscription bookkeeping outside `subscriptions.ts`.
- Do not hardcode `/api/realtime/ws` or reconnect backoff math outside `transport.ts`.
