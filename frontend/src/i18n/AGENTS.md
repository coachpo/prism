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
- Shell and route consumers: `../App.tsx`, `../components/GlobalPreferencesControls.tsx`, `../components/LanguageSwitcher.tsx`, `../components/WebSocketStatusIndicator.tsx`
- Shared formatting consumers: `../hooks/useTimezone.ts`, `../lib/timezone.ts`, `../lib/costing.ts`, and page helpers under `../pages/`
- Static label helpers for non-hook callers and known-label comparisons: `staticMessages.ts`

## CONVENTIONS

- Keep locale selection frontend-only.
- Keep `document.documentElement.lang` synchronized through `LocaleProvider.tsx`.
- Add new user-facing strings to the catalogs when they belong to reusable shell or route surfaces, including shared dual-strategy family and objective wording.
- Route shared formatting through `format.ts` or `useLocale()` instead of ad hoc `Intl.*` usage.
- Use `staticMessages.ts` when a non-hook caller needs locale-aware fallback labels, objective labels, or known-label comparisons.
- When doing upgrade work, backward compatibility with the pre-upgrade implementation is not a goal unless explicitly requested. Prefer the best current implementation shape over preserving the old one. Do not add compatibility shims, dual paths, or fallback behavior solely to preserve the old interface.

## ANTI-PATTERNS

- Do not bypass `useLocale()` for shared visible copy.
- Do not introduce another locale store outside `LocaleProvider.tsx`.
- Do not duplicate number, date, or relative-time helpers in page folders when `format.ts` already owns them.
- Do not duplicate known-label helpers or static fallback message lookups outside `staticMessages.ts`.
