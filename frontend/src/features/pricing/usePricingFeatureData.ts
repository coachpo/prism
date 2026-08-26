import { usePricingImportProtocol } from "./usePricingImportProtocol";
import { usePricingTemplateCollection } from "./usePricingTemplateCollection";
import { usePricingTemplateDetailReads } from "./usePricingTemplateDetailReads";
import { usePricingTemplateMutations } from "./usePricingTemplateMutations";

/**
 * Pricing page composition root. Collection/editor mutations, usage/history
 * reads, and the two-phase import protocol retain their own state owners.
 */
export function usePricingFeatureData(revision: number) {
  const collection = usePricingTemplateCollection(revision);
  const details = usePricingTemplateDetailReads();
  const {
    commitPricingTemplates,
    fetchPricingTemplates,
    ...collectionPage
  } = collection;
  const { loadPricingTemplateUsageForDelete, ...detailPage } = details;
  const mutations = usePricingTemplateMutations({
    commitPricingTemplates,
    fetchPricingTemplates,
    loadPricingTemplateUsageForDelete,
    pricingTemplateUsageError: details.pricingTemplateUsageError,
  });
  const importProtocol = usePricingImportProtocol({
    fetchPricingTemplates,
  });

  return {
    ...collectionPage,
    fetchPricingTemplates,
    ...detailPage,
    ...mutations,
    ...importProtocol,
  };
}
