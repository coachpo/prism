# FRONTEND PRICING TEMPLATES DOMAIN KNOWLEDGE BASE

## OVERVIEW
`pages/pricing-templates/` owns reusable pricing-template CRUD, usage lookups, CAS-safe edits, and delete conflict handling behind `../PricingTemplatesPage.tsx`. It stays selected-profile scoped because pricing templates belong to the current management profile.

## STRUCTURE
```
pricing-templates/
├── PricingTemplateDialog.tsx       # Create-edit dialog
├── DeletePricingTemplateDialog.tsx # Delete confirmation and in-use conflict display
├── PricingTemplatesTable.tsx       # List/table rendering
├── PricingTemplateUsageDialog.tsx  # Connection usage lookup dialog
├── pricingTemplateFormState.ts     # Form defaults, normalization, usage-row parsing
└── usePricingTemplatesPageData.ts  # Shared-cache bootstrap, CRUD, usage, and conflict flows
```

## WHERE TO LOOK

- Shared pricing-template cache and mutation patching: `usePricingTemplatesPageData.ts`
- Form normalization and decimal validation helpers: `pricingTemplateFormState.ts`
- Usage lookups before delete or review flows: `PricingTemplateUsageDialog.tsx`, `usePricingTemplatesPageData.ts`
- Conflict handling for in-use templates: `DeletePricingTemplateDialog.tsx`, `pricingTemplateFormState.ts`

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation, prefer manual confirmation over adding dedicated “proves not” tests; keep absence assertions only when the missing surface is itself a shipped contract or guardrail.
- Reuse the shared pricing-template cache in `@/lib/referenceData` for list bootstrap.
- Keep CAS-aware edit payload shaping in `usePricingTemplatesPageData.ts`; reopen or refetch on `409` instead of guessing merges.
- Parse delete conflicts and usage rows through `pricingTemplateFormState.ts` helpers instead of duplicating row normalization.
- Keep profile scope explicit in copy and behavior; this page follows the selected management profile rather than a global instance scope.

## LLM UPSTREAM MATRIX
- When work touches LLM upstream request or response logic, evaluate all six combinations: streaming and non-streaming for each `api_family` (`openai`, `gemini`, and `anthropic`).

## ANTI-PATTERNS

- Do not bypass usage lookups when delete conflicts need concrete connection rows.
- Do not hand-roll decimal-string normalization outside `pricingTemplateFormState.ts`.
- Do not refetch the entire page after every mutation when the local cache patch already keeps the list current.
- Do not document pricing templates as a global instance setting when the route is profile-scoped.
