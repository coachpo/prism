# FRONTEND PRICING TEMPLATES COMPATIBILITY CLUSTER

## OVERVIEW
`pages/pricing-templates/` keeps pricing-template dialogs still imported by the feature-owned `/route/pricing` route under `src/features/pricing/`. The feature route owns list rendering, form schema, CRUD orchestration, and cache patching.

## STRUCTURE
```text
pricing-templates/
├── DeletePricingTemplateDialog.tsx # Delete confirmation and in-use conflict display
└── PricingTemplateUsageDialog.tsx  # Connection usage lookup dialog
```

## WHERE TO LOOK
- Feature route, form schema, table, and mutation orchestration: `../../features/pricing/`
- Delete conflict display and usage dialog: `DeletePricingTemplateDialog.tsx`, `PricingTemplateUsageDialog.tsx`

## CONVENTIONS
- Reuse the shared pricing-template cache in `@/lib/referenceData` for list bootstrap.
- Keep CAS-aware edit payload shaping in the feature data hook; reopen or refetch on `409` instead of guessing merges.
- Keep profile scope explicit in copy and behavior; pricing templates follow the selected management profile rather than a global instance scope.
- Do not bypass usage lookups when delete conflicts need concrete connection rows.
