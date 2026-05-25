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

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep raw websocket message builders and parsing in `protocol.ts`.
- Keep channel subscription ref-count math in `subscriptions.ts`; `../websocket.ts` consumes the helpers but should not duplicate the logic.
- Keep URL construction and reconnect timing policy in `transport.ts`, while `../websocket.ts` owns the actual socket lifecycle and event handlers.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS
- Do not parse raw realtime JSON or hand-build subscribe or unsubscribe payloads outside `protocol.ts`.
- Do not duplicate ref-counted subscription bookkeeping outside `subscriptions.ts`.
- Do not hardcode `/api/realtime/ws` or reconnect backoff math outside `transport.ts`.
