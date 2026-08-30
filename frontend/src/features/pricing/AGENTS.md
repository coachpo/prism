# PRICING FEATURE KNOWLEDGE BASE

## OVERVIEW

`features/pricing/` owns the protected `/route/pricing` route, typed standard/tiered/peak_valley pricing-template contracts, resource lifecycles, import preview handoff, and card/window detail rendering. The backend remains the source of truth for kind validation, schedule geometry, CAS, revision persistence, and currency-migration blocking.

## STRUCTURE

- `PricingTemplatesTable.tsx`: pricing-template list table shell, type/card-count display, sorting/filtering, pagination, and row expansion orchestration.
- `PricingTemplateRatePanel.tsx`: complete role-keyed card and schedule summaries.
- `PricingTemplateUsagePanel.tsx`: bounded Terminal Target reference rendering.
- `PricingTemplateHistoryPanel.tsx`: append-only revision structure markers.
- `PricingCardFields.tsx`: shared five-component card editor used by every kind.
- `PricingPeakValleyFields.tsx`: two complete peak/offpeak cards and user-authored timezone/window fields.
- `pricingWindowDraft.ts`: pricing-local weekday and wall-clock conversion with no presets.
- `pricingSchemas.ts` / `pricingTierSchema.ts`: discriminated form state and payload normalization.
- `usePricingFeatureData.ts`: thin page coordinator for the independent collection, editor, detail-read, and import lifecycles.
- `usePricingTemplateCollection.ts`: pricing-template collection read, revision cache, sorting, and mutation commits.
- `usePricingTemplateMutations.ts`: editor detail/impact preflight, create/update CAS, and delete mutation workflow.
- `usePricingTemplateDetailReads.ts`: connection-usage and revision-history reads, including delete preflight usage state.
- `usePricingImportProtocol.ts`: preview-only then server-hash commit import protocol.
- `pricingDeletion.ts`: pricing-template delete-block decision from loading/error/dependency state.
- `pricingUsage.ts`: pricing-template connection-usage response parsing for detail and conflict views.
- `catalog/`: the shared models.dev source-linked pricing import, mounted by both `/route/pricing` and the model-detail Terminal Target action.
  - `useCatalogPricingImport.ts`: the preview/commit protocol owner. A `CatalogPricingSource` is either a persisted binding (`bound_model`) or explicit `coordinates` with optional display-only `modelConfigId`. The settled preview is keyed by `(sourceKey, targetsKey)`; any target-set change invalidates it and re-reads, and a rejected commit discards the preview and re-reads instead of retrying blind.
  - `CatalogPricingDialog.tsx`: the one dialog shell for both surfaces. It keeps discovery mounted beside the preview so the operator can re-choose, and renders explicit commit blockers rather than a bare disabled button.
  - `CatalogPricingPreviewPanel.tsx`: the shared preview display (both ends of the mapping, USD/PER_1M, five components per card, tier threshold, catalog revision, fetch stamp, labelled incompatibility reasons).
  - `CatalogOfferingDiscovery.tsx`: pricing-page model selection plus unique-match and bounded candidate search.
  - `catalogPricingPresentation.ts`: pure display rules (role order, five-component order, explicit `0` vs absent, stable reason labels, commit blockers). Every helper takes the locale bundle explicitly; nothing caches messages.
  - `useCatalogImportReferenceData.ts` / `useCatalogPricingImportEntry.ts`: dialog-scoped model/target reads and the pricing-page entry state plus post-commit cache refresh.

## CONVENTIONS

- The models.dev catalog price import is shared, not duplicated: `/route/pricing` and the model-detail action mount the same `catalog/` protocol hook and display components. Automation only discovers and prefills — a unique exact match may advance into the preview, but zero or multiple candidates always require an explicit human pick, nothing auto-selects the first or cheapest candidate, and nothing commits without an explicit action.
- Default target selection differs per surface and must stay that way: `/route/pricing` preselects **no** Terminal Target (template create/refresh only), while model detail preselects and locks the current target. Importing prices never binds the model.
- After a successful commit, drop the `pricingTemplates` and `connections` shared-reference caches and re-read authoritatively rather than optimistically patching, so the template list, target options, and target references agree immediately.
- An explicit catalog `0` renders as `0`; only an absent component renders the shared missing marker. Never leak a fail-closed reason enum key — `catalogIncompatibilityLabel` maps it to operator copy and keeps an unrecognised reason visible with its code.

- Keep the wire shape aligned with `backend/internal/httpapi/management/connections/`: `template_kind` is explicit; standard uses `card`, tiered uses `base_card` plus `tier.card`, and peak_valley uses `peak_card`, `offpeak_card`, and `schedule`. Legacy flat fields and provider presets are never authored.
- Keep tier form state in `pricingTierSchema.ts` and peak/valley form state in `PricingPeakValleyFields.tsx`; the parent pricing schema owns branch validation, specialty parity, and payload normalization. Browser validation normalizes input only; backend owns DST/window evaluation.
- Every configured card is complete for input/output and mirrors specialty configured/NULL shape across the role set. The UI must explain strict `>` tier selection and whole-request switching, and must not imply peak is numerically higher than offpeak.
- Use `@/shared/design-system` before primitive-only UI imports and route all visible copy through the locale messages. Missing tier evidence renders as an honest absent/unconfigured state, not zero.
- Keep request-log selection-state/card-role explanations in `../../pages/request-logs/` and currency-migration complete-card handling in the billing-currency settings leaf; do not duplicate those domain rules here.
- Keep the pricing table shell in `PricingTemplatesTable.tsx`; rate, usage, and history panels remain separate owners and none owns backend pricing/CAS rules.
- Keep form state/schema/validation/payload in `pricingSchemas.ts`; deletion blocking belongs to `pricingDeletion.ts`, and usage-response parsing belongs to `pricingUsage.ts`.
- Keep collection reads/cache in `usePricingTemplateCollection.ts`, editor and CAS/delete mutations in `usePricingTemplateMutations.ts`, usage/history reads in `usePricingTemplateDetailReads.ts`, and the two-phase import in `usePricingImportProtocol.ts`. `usePricingFeatureData.ts` only composes their page-facing contracts.
- Read failures stay distinct from an empty history or missing specialty price: history retains the last successful revisions with a staleness badge when refresh fails, and a first-read failure uses `OperatorErrorState` with retry. Unknown kinds and zero/missing peak windows render an error or explicit missing state.
- The edit dialog reads `/api/pricing-templates/{id}/impact` before enabling a save, so a failed impact preflight cannot be presented as zero references; peak/valley read panels and every history revision show concrete windows and role-keyed prices.

## VALIDATION

- Run `cd frontend && pnpm exec vitest run && pnpm run test:lib && pnpm run lint && pnpm run build` for the frontend gate.
- Pure tier form and payload cases belong in the existing Vitest/lib seam suites. The shared catalog import is journey-covered in `tests/e2e/model-catalog-pricing.spec.ts` and component-covered in `catalog/catalogPricingComponents.test.tsx`; do not add a separate Playwright spec for the pricing feature itself.
