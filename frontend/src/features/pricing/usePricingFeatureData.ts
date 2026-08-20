import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedPricingTemplates,
  setSharedPricingTemplates,
} from "@/lib/referenceData";
import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateImportRequest,
  PricingTemplateRevision,
} from "@/lib/types";
import {
  extractServerValidation,
  type ServerValidationResult,
} from "@/shared/forms/serverValidation";
import {
  buildPricingTemplateCreatePayload,
  buildPricingTemplateUpdatePayload,
  parsePricingTemplateUsageRows,
  type PricingTemplateFormValues,
} from "./pricingSchemas";
import type { PricingImportPreviewState } from "./PricingImportPreview";

export function usePricingFeatureData(revision: number) {
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>(
    [],
  );
  const [pricingTemplatesLoading, setPricingTemplatesLoading] = useState(false);
  const [pricingTemplatesError, setPricingTemplatesError] = useState<
    string | null
  >(null);
  const [pricingTemplateDialogOpen, setPricingTemplateDialogOpen] =
    useState(false);
  const [editingPricingTemplate, setEditingPricingTemplate] =
    useState<PricingTemplate | null>(null);
  const [pricingTemplatePreparingEditId, setPricingTemplatePreparingEditId] =
    useState<number | null>(null);
  const [pricingTemplateSaving, setPricingTemplateSaving] = useState(false);
  const [pricingTemplateServerError, setPricingTemplateServerError] = useState<ServerValidationResult | null>(null);
  const [pricingTemplateUsageRows, setPricingTemplateUsageRows] = useState<
    PricingTemplateConnectionUsageItem[]
  >([]);
  const [pricingTemplateUsageLoading, setPricingTemplateUsageLoading] =
    useState(false);
  const [pricingTemplateUsageLoadError, setPricingTemplateUsageLoadError] =
    useState<string | null>(null);
  const [pricingTemplateUsageTemplate, setPricingTemplateUsageTemplate] =
    useState<PricingTemplate | null>(null);
  const [deletePricingTemplateConfirm, setDeletePricingTemplateConfirmState] =
    useState<PricingTemplate | null>(null);
  const [deletePricingTemplateDisplay, setDeletePricingTemplateDisplay] =
    useState<PricingTemplate | null>(null);
  const [deletePricingTemplateConflict, setDeletePricingTemplateConflict] =
    useState<PricingTemplateConnectionUsageItem[] | null>(null);
  const [pricingTemplateUsageError, setPricingTemplateUsageError] =
    useState(false);
  const [pricingTemplateDeleting, setPricingTemplateDeleting] = useState(false);
  const [pricingTemplateImportDialogOpen, setPricingTemplateImportDialogOpen] =
    useState(false);
  const [pricingTemplateImporting, setPricingTemplateImporting] =
    useState(false);
  const [importPreview, setImportPreview] =
    useState<PricingImportPreviewState | null>(null);
  const [pricingTemplateHistoryTemplate, setPricingTemplateHistoryTemplate] =
    useState<PricingTemplate | null>(null);
  const [pricingTemplateHistoryRevisions, setPricingTemplateHistoryRevisions] =
    useState<PricingTemplateRevision[]>([]);
  const [pricingTemplateHistoryLoading, setPricingTemplateHistoryLoading] =
    useState(false);
  const navigate = useNavigate();

  const setDeletePricingTemplateConfirm = (
    template: PricingTemplate | null,
  ) => {
    if (template) setDeletePricingTemplateDisplay(template);
    setDeletePricingTemplateConfirmState(template);
  };
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
      const messages = getStaticMessages();
      setPricingTemplatesLoading(true);
      setPricingTemplatesError(null);
      try {
        setPricingTemplates(
          sortPricingTemplates(
            await getSharedPricingTemplates(revision, forceRefresh),
          ),
        );
      } catch (error) {
        const detail =
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.loadFailed;
        setPricingTemplatesError(detail);
        toast.error(detail);
      } finally {
        setPricingTemplatesLoading(false);
      }
    },
    [revision],
  );
  useEffect(() => {
    void fetchPricingTemplates();
  }, [fetchPricingTemplates]);

  const closePricingTemplateDialog = () => {
    setPricingTemplateDialogOpen(false);
    setPricingTemplateServerError(null);
  };
  const openCreatePricingTemplateDialog = () => {
    setEditingPricingTemplate(null);
    setPricingTemplatePreparingEditId(null);
    setPricingTemplateServerError(null);
    setPricingTemplateDialogOpen(true);
  };
  const handleEditPricingTemplate = async (
    templateSummary: PricingTemplate,
  ) => {
    const messages = getStaticMessages();
    setPricingTemplatePreparingEditId(templateSummary.id);
    try {
      setEditingPricingTemplate(
        await api.pricingTemplates.get(templateSummary.id),
      );
      setPricingTemplateServerError(null);
      setPricingTemplateDialogOpen(true);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.pricingTemplatesData.loadSingleFailed,
      );
    } finally {
      setPricingTemplatePreparingEditId(null);
    }
  };
  const handleSavePricingTemplate = async (
    values: PricingTemplateFormValues,
  ) => {
    const messages = getStaticMessages();
    setPricingTemplateServerError(null);
    setPricingTemplateSaving(true);
    try {
      if (editingPricingTemplate) {
        const updated = await api.pricingTemplates.update(
          editingPricingTemplate.id,
          buildPricingTemplateUpdatePayload(editingPricingTemplate, values),
        );
        commitPricingTemplates((current) =>
          current.map((template) =>
            template.id === editingPricingTemplate.id ? updated : template,
          ),
        );
        closePricingTemplateDialog();
        toast.success(messages.pricingTemplatesData.updated);
      } else {
        const created = await api.pricingTemplates.create(
          buildPricingTemplateCreatePayload(values),
        );
        commitPricingTemplates((current) => [created, ...current]);
        closePricingTemplateDialog();
        toast.success(messages.pricingTemplatesData.created, {
          action: {
            label: messages.pricingTemplatesData.continueToTarget,
            onClick: () => void navigate({ to: "/route/models" }),
          },
        });
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setPricingTemplateServerError({ issues: [], summary: messages.pricingTemplatesData.changedWhileEditing });
        await fetchPricingTemplates();
        return;
      }
      const validation = extractServerValidation(
        error,
        messages.pricingTemplatesData.saveFailed,
      );
      setPricingTemplateServerError(validation);
    } finally {
      setPricingTemplateSaving(false);
    }
  };
  const handleViewPricingTemplateHistory = async (
    template: PricingTemplate,
  ) => {
    const messages = getStaticMessages();
    setPricingTemplateHistoryTemplate(template);
    setPricingTemplateHistoryLoading(true);
    setPricingTemplateHistoryRevisions([]);
    try {
      setPricingTemplateHistoryRevisions(
        await api.pricingTemplates.revisions(template.id),
      );
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.pricingTemplatesData.historyLoadFailed,
      );
    } finally {
      setPricingTemplateHistoryLoading(false);
    }
  };
  const handleViewPricingTemplateUsage = async (template: PricingTemplate) => {
    const messages = getStaticMessages();
    setPricingTemplateUsageTemplate(template);
    setPricingTemplateUsageLoading(true);
    setPricingTemplateUsageLoadError(null);
    try {
      const data = await api.pricingTemplates.connections(template.id);
      setPricingTemplateUsageRows(data.items);
      setPricingTemplateUsageLoadError(null);
    } catch (error) {
      const detail =
        error instanceof Error
          ? error.message
          : messages.pricingTemplatesData.loadUsageFailed;
      toast.error(detail);
      setPricingTemplateUsageLoadError(detail);
      setPricingTemplateUsageRows([]);
    } finally {
      setPricingTemplateUsageLoading(false);
    }
  };
  const handleDeletePricingTemplateClick = async (
    template: PricingTemplate,
  ) => {
    const messages = getStaticMessages();
    setDeletePricingTemplateConfirm(template);
    setDeletePricingTemplateConflict(null);
    setPricingTemplateUsageError(false);
    setPricingTemplateUsageLoading(true);
    try {
      const data = await api.pricingTemplates.connections(template.id);
      setPricingTemplateUsageRows(data.items);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.pricingTemplatesData.loadUsageFailed,
      );
      setPricingTemplateUsageError(true);
      setPricingTemplateUsageRows([]);
    } finally {
      setPricingTemplateUsageLoading(false);
    }
  };
  const handleDeletePricingTemplate = async () => {
    const messages = getStaticMessages();
    if (!deletePricingTemplateConfirm) return;
    setPricingTemplateDeleting(true);
    try {
      if (pricingTemplateUsageError) {
        toast.error(messages.pricingTemplatesData.loadUsageFailed);
        return;
      }
      await api.pricingTemplates.delete(deletePricingTemplateConfirm.id);
      commitPricingTemplates((current) =>
        current.filter(
          (template) => template.id !== deletePricingTemplateConfirm.id,
        ),
      );
      toast.success(messages.pricingTemplatesData.deleted);
      setDeletePricingTemplateConfirm(null);
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setDeletePricingTemplateConflict(
          parsePricingTemplateUsageRows(error.detail),
        );
        toast.error(messages.pricingTemplatesData.inUseCannotDelete);
      } else {
        toast.error(
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.deleteFailed,
        );
      }
    } finally {
      setPricingTemplateDeleting(false);
    }
  };
  /**
   * Phase one of the two-phase import (SPEC 7.6). The preview is surfaced on
   * the page as a diff the operator confirms; nothing is written here. An
   * uncommittable or error-carrying preview still lands on screen so the
   * operator can see why, but its commit stays blocked.
   */
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
      const body =
        error instanceof ApiError &&
        error.detail &&
        typeof error.detail === "object"
          ? (error.detail as { errors?: Array<{ detail?: string }> })
          : null;
      toast.error(
        body?.errors?.[0]?.detail ||
          (error instanceof Error
            ? error.message
            : messages.common.requestFailed),
      );
      return false;
    } finally {
      setPricingTemplateImporting(false);
    }
  };

  /** Phase two: commit strictly with the server-issued preview hash. */
  const commitImportPreview = async () => {
    const messages = getStaticMessages();
    if (!importPreview) return false;
    const { request, response } = importPreview;
    // Fail closed: a preview carrying errors or without a hash never commits.
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
        schema_version: 2,
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
      const body =
        error instanceof ApiError &&
        error.detail &&
        typeof error.detail === "object"
          ? (error.detail as { errors?: Array<{ detail?: string }> })
          : null;
      toast.error(
        body?.errors?.[0]?.detail ||
          (error instanceof Error
            ? error.message
            : messages.common.requestFailed),
      );
      return false;
    } finally {
      setPricingTemplateImporting(false);
    }
  };
  return {
    cancelImportPreview: () => setImportPreview(null),
    commitImportPreview,
    importPreview,
    closePricingTemplateDialog,
    deletePricingTemplateConfirm,
    deletePricingTemplateDisplay,
    deletePricingTemplateConflict,
    editingPricingTemplate,
    handleDeletePricingTemplate,
    handleDeletePricingTemplateClick,
    handleEditPricingTemplate,
    handleImportPricingTemplates,
    handleSavePricingTemplate,
    handleViewPricingTemplateHistory,
    handleViewPricingTemplateUsage,
    openCreatePricingTemplateDialog,
    pricingTemplateDeleting,
    pricingTemplateDialogOpen,
    pricingTemplateImportDialogOpen,
    pricingTemplateHistoryLoading,
    pricingTemplateHistoryRevisions,
    pricingTemplateHistoryTemplate,
    pricingTemplateImporting,
    pricingTemplatePreparingEditId,
    pricingTemplateSaving,
    pricingTemplateServerError,
    pricingTemplateUsageError,
    pricingTemplateUsageLoadError,
    pricingTemplateUsageLoading,
    pricingTemplateUsageRows,
    pricingTemplateUsageTemplate,
    pricingTemplates,
    pricingTemplatesError,
    pricingTemplatesLoading,
    fetchPricingTemplates,
    setDeletePricingTemplateConfirm,
    setDeletePricingTemplateConflict,
    setPricingTemplateDialogOpen,
    setPricingTemplateImportDialogOpen,
  };
}

function sortPricingTemplates(templates: PricingTemplate[]) {
  return [...templates].sort(
    (left, right) =>
      new Date(right.updated_at).getTime() -
        new Date(left.updated_at).getTime() || right.id - left.id,
  );
}
