# FRONTEND I18N KNOWLEDGE BASE

## OVERVIEW
`src/i18n/` owns Prism's frontend-only zh-CN locale boundary, including the single locale catalog, shared formatting helpers, and static label helpers for non-hook code.

## STRUCTURE
```
i18n/
├── LocaleProvider.tsx   # Runtime zh-CN state and document.lang sync
├── locale-context.ts    # Shared React context contract
├── useLocale.ts         # Locale, messages, and format helper hook
├── format.ts            # Intl helpers for formatting and collation
├── staticMessages.ts    # Static label lookup and known-label helpers for non-hook callers
└── messages/
    ├── index.ts
    └── zh-CN.ts
```

## WHERE TO LOOK

- Provider mount point: `../main.tsx`
- Shell and route consumers: `../App.tsx`, `../components/ThemeToggle.tsx`, `../components/layout/page.tsx`
- Shared formatting consumers: `../hooks/useTimezone.ts`, `../lib/timezone.ts`, `../lib/costing.ts`, and page helpers under `../pages/`
- Static label helpers for non-hook callers, fallback labels, Ban Policy labels, and known-label comparisons: `staticMessages.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep the frontend locale fixed to `zh-CN`.
- Keep `document.documentElement.lang` synchronized through `LocaleProvider.tsx`.
- Keep `messages/zh-CN.ts` as the only language and `Messages` type source; `messages/index.ts` is the public re-export.
- Add new user-facing strings to the zh-CN catalog when they belong to reusable shell or route surfaces, including shared explicit Ban Policy wording and final-target observability labels.
- Route shared formatting through `format.ts` or `useLocale()` instead of ad hoc `Intl.*` usage.
- Use `staticMessages.ts` when a non-hook caller needs locale-aware fallback labels, Ban Policy labels, or known-label comparisons instead of importing React hooks.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`) and Responses (`/v1/responses`), Gemini, and Anthropic.

## ANTI-PATTERNS

- Do not bypass `useLocale()` for shared visible copy.
- Do not introduce another locale store outside `LocaleProvider.tsx`.
- Do not duplicate number, date, or relative-time helpers in page folders when `format.ts` already owns them.
- Do not duplicate known-label helpers or static fallback message lookups outside `staticMessages.ts`.
