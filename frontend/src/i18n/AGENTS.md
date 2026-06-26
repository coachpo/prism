# FRONTEND I18N KNOWLEDGE BASE

## OVERVIEW
`src/i18n/` owns Prism's frontend-only locale boundary, including locale selection, persistence, large locale catalogs, shared formatting helpers, and static label helpers for non-hook code.

## STRUCTURE
```
i18n/
├── LocaleProvider.tsx   # Runtime locale state and document.lang sync
├── locale-context.ts    # Shared React context contract
├── useLocale.ts         # Locale, messages, and format helper hook
├── format.ts            # Intl helpers for formatting and collation
├── staticMessages.ts    # Static label lookup and known-label helpers for non-hook callers
└── messages/
    ├── en.ts
    └── zh-CN.ts
```

## WHERE TO LOOK

- Provider mount point: `../main.tsx`
- Shell and route consumers: `../App.tsx`, `../components/LanguageSwitcher.tsx`, `../components/ThemeToggle.tsx`, `../components/WebSocketStatusIndicator.tsx`, `../components/layout/page.tsx`
- Shared formatting consumers: `../hooks/useTimezone.ts`, `../lib/timezone.ts`, `../lib/costing.ts`, and page helpers under `../pages/`
- Static label helpers for non-hook callers, fallback labels, Ban Policy labels, and known-label comparisons: `staticMessages.ts`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Keep locale selection frontend-only.
- Keep `document.documentElement.lang` synchronized through `LocaleProvider.tsx`.
- Keep the reusable message catalogs in `messages/en.ts` and `messages/zh-CN.ts` as the primary user-facing string store.
- Persist locale selection through `LocaleProvider.tsx` and its `localStorage` key instead of introducing a second preference store.
- Add new user-facing strings to the catalogs when they belong to reusable shell or route surfaces, including shared explicit Ban Policy wording and final-target observability labels.
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
