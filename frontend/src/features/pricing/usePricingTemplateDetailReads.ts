import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateRevision,
} from "@/lib/types";

export function usePricingTemplateDetailReads() {
  const [pricingTemplateUsageRows, setPricingTemplateUsageRows] = useState<
    PricingTemplateConnectionUsageItem[]
  >([]);
  const [pricingTemplateUsageLoading, setPricingTemplateUsageLoading] =
    useState(false);
  const [pricingTemplateUsageLoadError, setPricingTemplateUsageLoadError] =
    useState<string | null>(null);
  const [pricingTemplateUsageTemplate, setPricingTemplateUsageTemplate] =
    useState<PricingTemplate | null>(null);
  const [pricingTemplateUsageError, setPricingTemplateUsageError] =
    useState(false);
  const [pricingTemplateHistoryTemplate, setPricingTemplateHistoryTemplate] =
    useState<PricingTemplate | null>(null);
  const [pricingTemplateHistoryRevisions, setPricingTemplateHistoryRevisions] =
    useState<PricingTemplateRevision[]>([]);
  const [pricingTemplateHistoryLoading, setPricingTemplateHistoryLoading] =
    useState(false);
  const [pricingTemplateHistoryError, setPricingTemplateHistoryError] =
    useState<string | null>(null);
  const usageGeneration = useRef(0);
  const historyGeneration = useRef(0);

  const handleViewPricingTemplateUsage = useCallback(
    async (template: PricingTemplate) => {
      const generation = ++usageGeneration.current;
      const messages = getStaticMessages();
      setPricingTemplateUsageTemplate(template);
      setPricingTemplateUsageLoading(true);
      setPricingTemplateUsageLoadError(null);
      try {
        const data = await api.pricingTemplates.connections(template.id);
        if (generation !== usageGeneration.current) return;
        setPricingTemplateUsageRows(data.items);
        setPricingTemplateUsageLoadError(null);
      } catch (error) {
        if (generation !== usageGeneration.current) return;
        const detail =
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.loadUsageFailed;
        toast.error(detail);
        setPricingTemplateUsageLoadError(detail);
        setPricingTemplateUsageRows([]);
      } finally {
        if (generation === usageGeneration.current) {
          setPricingTemplateUsageLoading(false);
        }
      }
    },
    [],
  );

  const loadPricingTemplateUsageForDelete = useCallback(
    async (template: PricingTemplate) => {
      const generation = ++usageGeneration.current;
      const messages = getStaticMessages();
      setPricingTemplateUsageTemplate(template);
      setPricingTemplateUsageLoading(true);
      setPricingTemplateUsageLoadError(null);
      setPricingTemplateUsageError(false);
      try {
        const data = await api.pricingTemplates.connections(template.id);
        if (generation !== usageGeneration.current) return null;
        setPricingTemplateUsageRows(data.items);
        return data.items;
      } catch (error) {
        if (generation !== usageGeneration.current) return null;
        toast.error(
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.loadUsageFailed,
        );
        setPricingTemplateUsageError(true);
        setPricingTemplateUsageRows([]);
        return null;
      } finally {
        if (generation === usageGeneration.current) {
          setPricingTemplateUsageLoading(false);
        }
      }
    },
    [],
  );

  const handleViewPricingTemplateHistory = useCallback(
    async (template: PricingTemplate) => {
      const generation = ++historyGeneration.current;
      const messages = getStaticMessages();
      const sameTemplate = pricingTemplateHistoryTemplate?.id === template.id;
      setPricingTemplateHistoryTemplate(template);
      setPricingTemplateHistoryLoading(true);
      setPricingTemplateHistoryError(null);
      if (!sameTemplate) setPricingTemplateHistoryRevisions([]);
      try {
        const revisions = await api.pricingTemplates.revisions(template.id);
        if (generation !== historyGeneration.current) return;
        setPricingTemplateHistoryRevisions(revisions);
      } catch (error) {
        if (generation !== historyGeneration.current) return;
        const detail =
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.historyLoadFailed;
        setPricingTemplateHistoryError(detail);
        toast.error(detail);
      } finally {
        if (generation === historyGeneration.current) {
          setPricingTemplateHistoryLoading(false);
        }
      }
    },
    [pricingTemplateHistoryTemplate],
  );

  return {
    handleViewPricingTemplateHistory,
    handleViewPricingTemplateUsage,
    loadPricingTemplateUsageForDelete,
    pricingTemplateHistoryError,
    pricingTemplateHistoryLoading,
    pricingTemplateHistoryRevisions,
    pricingTemplateHistoryTemplate,
    pricingTemplateUsageError,
    pricingTemplateUsageLoadError,
    pricingTemplateUsageLoading,
    pricingTemplateUsageRows,
    pricingTemplateUsageTemplate,
  };
}
