# PRICING FEATURE KNOWLEDGE BASE

## OVERVIEW

`features/pricing/` owns the protected `/route/pricing` route, typed standard/tiered/peak_valley pricing-template form/list orchestration, import preview handoff, and card/window detail rendering. The backend remains the source of truth for kind validation, schedule geometry, CAS, revision persistence, and currency-migration blocking.

## STRUCTURE

- `PricingTemplatesTable.tsx`: pricing-template list table shell, type/card-count display, sorting/filtering, pagination, and row expansion orchestration.
- `PricingTemplateRatePanel.tsx`: complete role-keyed card and schedule summaries.
- `PricingTemplateUsagePanel.tsx`: bounded Terminal Target reference rendering.
- `PricingTemplateHistoryPanel.tsx`: append-only revision structure markers.
- `PricingCardFields.tsx`: shared five-component card editor used by every kind.
- `PricingPeakValleyFields.tsx`: two complete peak/offpeak cards and user-authored timezone/window fields.
- `pricingWindowDraft.ts`: pricing-local weekday and wall-clock conversion with no presets.
- `pricingSchemas.ts` / `pricingTierSchema.ts`: discriminated form state and payload normalization.
- `pricingDeletion.ts`: pricing-template delete-block decision from loading/error/dependency state.
- `pricingUsage.ts`: pricing-template connection-usage response parsing for detail and conflict views.

## CONVENTIONS

- Keep the wire shape aligned with `backend/internal/httpapi/management/connections/`: `template_kind` is explicit; standard uses `card`, tiered uses `base_card` plus `tier.card`, and peak_valley uses `peak_card`, `offpeak_card`, and `schedule`. Legacy flat fields and provider presets are never authored.
- Keep tier form state in `pricingTierSchema.ts` and peak/valley form state in `PricingPeakValleyFields.tsx`; the parent pricing schema owns branch validation, specialty parity, and payload normalization. Browser validation normalizes input only; backend owns DST/window evaluation.
- Every configured card is complete for input/output and mirrors specialty configured/NULL shape across the role set. The UI must explain strict `>` tier selection and whole-request switching, and must not imply peak is numerically higher than offpeak.
- Use `@/shared/design-system` before primitive-only UI imports and route all visible copy through the locale messages. Missing tier evidence renders as an honest absent/unconfigured state, not zero.
- Keep request-log selection-state/card-role explanations in `../../pages/request-logs/` and currency-migration complete-card handling in the billing-currency settings leaf; do not duplicate those domain rules here.
- Keep the pricing table shell in `PricingTemplatesTable.tsx`; rate, usage, and history panels remain separate owners and none owns backend pricing/CAS rules.
- Keep form state/schema/validation/payload in `pricingSchemas.ts`; deletion blocking belongs to `pricingDeletion.ts`, and usage-response parsing belongs to `pricingUsage.ts`.
- Read failures stay distinct from an empty history or missing specialty price: history retains the last successful revisions with a staleness badge when refresh fails, and a first-read failure uses `OperatorErrorState` with retry. Unknown kinds and zero/missing peak windows render an error or explicit missing state.
- The edit dialog reads `/api/pricing-templates/{id}/impact` before enabling a save, so a failed impact preflight cannot be presented as zero references; peak/valley read panels and every history revision show concrete windows and role-keyed prices.

## VALIDATION

- Run `cd frontend && pnpm exec vitest run && pnpm run test:lib && pnpm run lint && pnpm run build` for the frontend gate.
- Pure tier form and payload cases belong in the existing Vitest/lib seam suites; do not add a Playwright spec for this feature.
