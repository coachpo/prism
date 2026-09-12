import { useState } from "react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { extractServerValidation } from "@/shared/forms/serverValidation";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  PricingTemplateImportRequest,
  PricingTemplateImportResponse,
} from "@/lib/types";

export type PricingImportPreviewState = {
  request: PricingTemplateImportRequest;
  response: PricingTemplateImportResponse;
};

interface UsePricingImportProtocolInput {
  fetchPricingTemplates: (forceRefresh?: boolean) => void | Promise<void>;
}

export function usePricingImportProtocol({
  fetchPricingTemplates,
}: UsePricingImportProtocolInput) {
  const [pricingTemplateImportDialogOpen, setPricingTemplateImportDialogOpen] =
    useState(false);
  const [pricingTemplateImporting, setPricingTemplateImporting] =
    useState(false);
  const [pricingTemplateImportError, setPricingTemplateImportError] = useState<string | null>(null);
  const [importPreview, setImportPreview] =
    useState<PricingImportPreviewState | null>(null);

  const handleImportPricingTemplates = async (
    request: PricingTemplateImportRequest,
  ) => {
    const messages = getStaticMessages();
    setPricingTemplateImportError(null);
    setPricingTemplateImporting(true);
    try {
      const response = await api.pricingTemplates.importTemplates(request);
      setImportPreview({ request, response });
      setPricingTemplateImportDialogOpen(false);
      if (response.errors.length > 0 || !response.committable) {
        return false;
      }
      return true;
    } catch (error) {
      setPricingTemplateImportError(extractServerValidation(error, messages.pricing.importFailed).summary);
      return false;
    } finally {
      setPricingTemplateImporting(false);
    }
  };

  const commitImportPreview = async () => {
    const messages = getStaticMessages();
    if (!importPreview) return false;
    const { request, response } = importPreview;
    if (
      response.errors.length > 0 ||
      !response.committable ||
      !response.preview_hash
    ) {
      toast.error(messages.common.requestFailed);
      return false;
    }
    setPricingTemplateImporting(true);
    try {
      const result = await api.pricingTemplates.importCommit({
        schema_version: 3,
        mode: request.mode,
        templates: request.templates,
        preview_hash: response.preview_hash,
      });
      await fetchPricingTemplates(true);
      toast.success(
        messages.pricing.importResultSummary(
          result.created,
          result.updated,
          result.skipped.length,
        ),
      );
      setImportPreview(null);
      return true;
    } catch (error) {
      toast.error(extractServerValidation(error, messages.pricing.importFailed).summary);
      return false;
    } finally {
      setPricingTemplateImporting(false);
    }
  };

  return {
    cancelImportPreview: () => setImportPreview(null),
    commitImportPreview,
    handleImportPricingTemplates,
    importPreview,
    pricingTemplateImportDialogOpen,
    pricingTemplateImportError,
    pricingTemplateImporting,
    setPricingTemplateImportDialogOpen: (open: boolean) => {
      setPricingTemplateImportError(null);
      setPricingTemplateImportDialogOpen(open);
    },
  };
}
