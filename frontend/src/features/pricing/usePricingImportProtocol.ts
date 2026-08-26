import { useState } from "react";
import { toast } from "sonner";

import { ApiError, api } from "@/lib/api";
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
  const [importPreview, setImportPreview] =
    useState<PricingImportPreviewState | null>(null);

  const handleImportPricingTemplates = async (
    request: PricingTemplateImportRequest,
  ) => {
    const messages = getStaticMessages();
    setPricingTemplateImporting(true);
    try {
      const response = await api.pricingTemplates.importTemplates(request);
      setImportPreview({ request, response });
      setPricingTemplateImportDialogOpen(false);
      if (response.errors.length > 0 || !response.committable) {
        toast.error(
          response.errors[0]?.detail ?? messages.common.requestFailed,
        );
        return false;
      }
      return true;
    } catch (error) {
      toast.error(importErrorMessage(error, messages.common.requestFailed));
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
      toast.error(importErrorMessage(error, messages.common.requestFailed));
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
    pricingTemplateImporting,
    setPricingTemplateImportDialogOpen,
  };
}

function importErrorMessage(error: unknown, fallback: string) {
  if (
    error instanceof ApiError &&
    error.detail &&
    typeof error.detail === "object"
  ) {
    const body = error.detail as { errors?: Array<{ detail?: string }> };
    if (body.errors?.[0]?.detail) return body.errors[0].detail;
  }
  return error instanceof Error ? error.message : fallback;
}
