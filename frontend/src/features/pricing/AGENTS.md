# PRICING FEATURE KNOWLEDGE BASE

## OVERVIEW
`features/pricing/` owns the protected `/route/pricing` route, pricing-template form/list orchestration, import preview handoff, and the single-threshold two-card pricing UI. The backend remains the source of truth for tier validation, CAS, revision persistence, and currency-migration blocking.

## CONVENTIONS
- Keep the wire shape aligned with `backend/internal/httpapi/management/connections/`: the nested object is singular `tier`; reads return `tier: null` when unconfigured. Create/import may omit it, and update omission preserves while `null` clears.
- Keep tier form state in `pricingTierSchema.ts` and field composition in `PricingTierFields.tsx`; the parent pricing schema owns base/tier parity and payload normalization.
- A configured tier is a complete five-component mirror, including reasoning. The UI must explain strict `>` threshold selection and whole-request switching, never marginal billing.
- Use `@/shared/design-system` before primitive-only UI imports and route all visible copy through the locale messages. Missing tier evidence renders as an honest absent/unconfigured state, not zero.
- Keep request-log tier explanations in `../../pages/request-logs/` and currency-migration conflict rendering in the billing-currency settings leaf; do not duplicate those domain rules here.

## VALIDATION
- Run `cd frontend && pnpm exec vitest run && pnpm run test:lib && pnpm run lint && pnpm run build` for the frontend gate.
- Pure tier form and payload cases belong in the existing Vitest/lib seam suites; do not add a Playwright spec for this feature.
