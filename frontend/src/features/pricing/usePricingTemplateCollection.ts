import { useCallback, useEffect, useRef, useState } from "react";

import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedPricingTemplates,
  setSharedPricingTemplates,
} from "@/lib/referenceData";
import type { PricingTemplate } from "@/lib/types";

function sortPricingTemplates(templates: PricingTemplate[]) {
  return [...templates].sort(
    (left, right) =>
      new Date(right.updated_at).getTime() -
        new Date(left.updated_at).getTime() || right.id - left.id,
  );
}

export function usePricingTemplateCollection(revision: number) {
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>(
    [],
  );
  const [pricingTemplatesLoading, setPricingTemplatesLoading] = useState(false);
  const [pricingTemplatesError, setPricingTemplatesError] = useState<
    string | null
  >(null);
  const requestGeneration = useRef(0);

  const commitPricingTemplates = useCallback(
    (updater: (current: PricingTemplate[]) => PricingTemplate[]) => {
      setPricingTemplates((current) => {
        const next = sortPricingTemplates(updater(current));
        setSharedPricingTemplates(revision, next);
        return next;
      });
    },
    [revision],
  );

  const fetchPricingTemplates = useCallback(
    async (forceRefresh = false) => {
      const generation = ++requestGeneration.current;
      const messages = getStaticMessages();
      setPricingTemplatesLoading(true);
      setPricingTemplatesError(null);
      try {
        const next = sortPricingTemplates(
          await getSharedPricingTemplates(revision, forceRefresh),
        );
        if (generation !== requestGeneration.current) return;
        setPricingTemplates(next);
        setPricingTemplatesError(null);
      } catch (error) {
        if (generation !== requestGeneration.current) return;
        const detail =
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.loadFailed;
        setPricingTemplatesError(detail);
        toast.error(detail);
      } finally {
        if (generation === requestGeneration.current) {
          setPricingTemplatesLoading(false);
        }
      }
    },
    [revision],
  );

  useEffect(() => {
    void fetchPricingTemplates();
  }, [fetchPricingTemplates]);

  return {
    commitPricingTemplates,
    fetchPricingTemplates,
    pricingTemplates,
    pricingTemplatesError,
    pricingTemplatesLoading,
  };
}
