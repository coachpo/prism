# Locale and formatting

- Keep `LocaleProvider.tsx` fixed to `zh-CN` and responsible for `document.documentElement.lang`. `messages/zh-CN.ts` supplies the catalog and `Messages` type; `messages/index.ts` re-exports them.
- Reusable visible copy goes through `useLocale()`. Non-hook consumers use `staticMessages.ts`, including known-label comparisons and fallback labels, rather than importing React hooks or caching a locale bundle.
- Shared numeric/date/collation formatting belongs to `format.ts`. Timestamp hooks use `../hooks/useTimezone.ts`; timezone preference caching, offset, and preview helpers belong to `../lib/timezone.ts`.
- Preserve distinct terms for entry model, Model Target, final target model, and upstream model ID. They name different identities in routing and retained evidence.
