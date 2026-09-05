# Cross-route helpers

- Keep shared code cross-route. Feature-specific schemas, payloads, and mutation hooks stay with their feature; reusable operator components belong to [design-system](design-system/AGENTS.md).
- `api/queryKeys.ts` and `api/queryInvalidation.ts` keep Default-profile and global query scope explicit. Headers accepted only for compatibility do not justify cache scope; runtime traffic is a separate domain.
- `forms/serverValidation.ts` preserves backend field paths so forms can display precise errors.
- `table/useAppendCandidatePager.ts` owns source-qualified keys/generations, replace/append/retry, deduplication, and revision rollover. Rows, revision, and adapter evidence commit atomically; late responses cannot update freshness after their rows are rejected. Models.dev/Pi adapters own DTOs, copy, and selection policy.
- Keep pure sorting/page calculations in `table/operationalTableState.ts`; table chrome and pagination controls consume those helpers. Expose intentionally shared helpers through `index.ts`.
