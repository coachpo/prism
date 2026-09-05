# Pricing templates and catalog import

- Keep collection/cache, editor/CAS/delete, usage/history reads, and preview/commit import in `usePricingTemplateCollection.ts`, `usePricingTemplateMutations.ts`, `usePricingTemplateDetailReads.ts`, and `usePricingImportProtocol.ts`. `usePricingFeatureData.ts` composes their contracts.
- `pricingSchemas.ts` owns discriminated form/payload validation; `pricingTierSchema.ts` and `PricingPeakValleyFields.tsx` own their branch drafts. Standard uses `card`, tiered uses `base_card` plus `tier.card`, and peak/valley uses two complete role cards and a schedule. Preserve explicit zero versus missing prices and specialty configured/NULL parity across cards.
- Tier selection is strict `>` and switches the whole request. Peak need not cost more than offpeak; the backend owns schedule geometry, DST evaluation, and revisions.
- Keep the table shell separate from rate, usage, and history panels. Edit saves wait for a successful impact read; unknown dependency state is not zero references. Delete blocking belongs to `pricingDeletion.ts`, and concrete conflict rows come from `pricingUsage.ts`/detail reads.
- Failed history refresh retains last-good revisions with staleness; failed first reads show retryable error. Multi-card summaries retain every role and concrete schedule windows.

The shared `catalog/` import serves `/route/pricing` and model-detail Terminal Target actions.

- `catalog/useCatalogPricingImport.ts` keys preview and drift acknowledgement to the exact source/target set. Any target change invalidates the preview; rejected commits discard it and re-read.
- Discovery may advance one exact match to preview, but ambiguous/absent matches require explicit selection; commits always require an operator action. The pricing route preselects no Terminal Target, while model detail locks the current target. Importing prices does not bind the model.
- `catalog/CatalogPricingDialog.tsx` owns both surfaces. Keep discovery alongside preview, display commit blockers, lock mutable controls during commit, and use the response's stored template name for success copy.
- Success drops pricing-template/connection reference caches and re-reads. `catalog/catalogPricingPresentation.ts` owns role/component ordering and localized incompatibility reasons; unknown reasons remain visible with their code.

Use the existing form/lib seams, `catalog/catalogPricingComponents.test.tsx`, and `../../../tests/e2e/model-catalog-pricing.spec.ts` for the affected boundary.
