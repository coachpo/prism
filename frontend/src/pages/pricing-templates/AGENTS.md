# FRONTEND PRICING TEMPLATES COMPATIBILITY CLUSTER

## OVERVIEW
`pages/pricing-templates/` keeps pricing-template dialogs still imported by the feature-owned `/route/pricing` route under `src/features/pricing/`. The feature route owns list rendering while the named pricing feature owners hold form, collection, editor, detail-read, import, and cache contracts.

## STRUCTURE
```text
pricing-templates/
└── DeletePricingTemplateDialog.tsx # Delete confirmation and in-use conflict display
```

## WHERE TO LOOK
- Feature route, form schema, table, and named lifecycle owners: `../../features/pricing/`
- Delete conflict display: `DeletePricingTemplateDialog.tsx`
- Delete-block decision: `../../features/pricing/pricingDeletion.ts`
- Connection usage lookup that feeds those conflict rows: `../../features/pricing/usePricingTemplateDetailReads.ts` (`handleViewPricingTemplateUsage`), `../../features/pricing/PricingTemplatesTable.tsx`

## CONVENTIONS
- For UI/UX, frontend visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation changes, follow `frontend/DESIGN.md`: use `@/shared/design-system` before `@/components/ui`, preserve the Google Admin Console / Material Design 3 operator direction, use semantic tokens, operator surface classes, density variables, and required operator components, keep route state and API calls out of design-system components, and avoid adding compatibility wrappers under `@/components`.
- Do not add decorative gradients, blur blobs, heavy shadows, marketing hero layouts, raw Tailwind status colors, page-local color blends, or ad hoc dark-mode overrides outside the `frontend/DESIGN.md` contract.
- Reuse the shared pricing-template cache in `@/lib/referenceData` for list bootstrap.
- Keep CAS-aware edit payload shaping in `pricingSchemas.ts` and the editor lifecycle in `usePricingTemplateMutations.ts`; reopen or refetch on `409` instead of guessing merges.
- Keep pricing-template copy neutral while preserving the backend's pinned profile id `1` behavior.
- Do not bypass usage lookups when delete conflicts need concrete connection rows.
