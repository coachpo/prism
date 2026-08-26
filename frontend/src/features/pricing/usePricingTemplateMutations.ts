import { useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";

import { ApiError, api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { extractServerValidation, type ServerValidationResult } from "@/shared/forms/serverValidation";
import type { PricingTemplate } from "@/lib/types";
import {
  buildPricingTemplateCreatePayload,
  buildPricingTemplateUpdatePayload,
  type PricingTemplateFormValues,
} from "./pricingSchemas";
import { parsePricingTemplateUsageRows } from "./pricingUsage";
import type { PricingTemplateConnectionUsageItem } from "@/lib/types";

type PricingTemplateCollectionInput = {
  commitPricingTemplates: (
    updater: (current: PricingTemplate[]) => PricingTemplate[],
  ) => void;
  fetchPricingTemplates: (forceRefresh?: boolean) => void | Promise<void>;
};

type PricingTemplateUsageInput = {
  loadPricingTemplateUsageForDelete: (
    template: PricingTemplate,
  ) => Promise<PricingTemplateConnectionUsageItem[] | null>;
  pricingTemplateUsageError: boolean;
};

export function usePricingTemplateMutations({
  commitPricingTemplates,
  fetchPricingTemplates,
  loadPricingTemplateUsageForDelete,
  pricingTemplateUsageError,
}: PricingTemplateCollectionInput & PricingTemplateUsageInput) {
  const navigate = useNavigate();
  const [pricingTemplateDialogOpen, setPricingTemplateDialogOpen] =
    useState(false);
  const [editingPricingTemplate, setEditingPricingTemplate] =
    useState<PricingTemplate | null>(null);
  const [pricingTemplatePreparingEditId, setPricingTemplatePreparingEditId] =
    useState<number | null>(null);
  const [pricingTemplateImpact, setPricingTemplateImpact] =
    useState<import("@/lib/types").PricingTemplateImpact | null>(null);
  const [pricingTemplateImpactLoading, setPricingTemplateImpactLoading] =
    useState(false);
  const [pricingTemplateImpactError, setPricingTemplateImpactError] =
    useState<string | null>(null);
  const [pricingTemplateSaving, setPricingTemplateSaving] = useState(false);
  const [pricingTemplateServerError, setPricingTemplateServerError] =
    useState<ServerValidationResult | null>(null);
  const [deletePricingTemplateConfirm, setDeletePricingTemplateConfirmState] =
    useState<PricingTemplate | null>(null);
  const [deletePricingTemplateDisplay, setDeletePricingTemplateDisplay] =
    useState<PricingTemplate | null>(null);
  const [deletePricingTemplateConflict, setDeletePricingTemplateConflict] =
    useState<PricingTemplateConnectionUsageItem[] | null>(null);
  const [pricingTemplateDeleting, setPricingTemplateDeleting] = useState(false);
  const editGeneration = useRef(0);

  const setDeletePricingTemplateConfirm = (
    template: PricingTemplate | null,
  ) => {
    if (template) setDeletePricingTemplateDisplay(template);
    setDeletePricingTemplateConfirmState(template);
  };

  const closePricingTemplateDialog = () => {
    setPricingTemplateDialogOpen(false);
    setPricingTemplateServerError(null);
  };

  const openCreatePricingTemplateDialog = () => {
    setEditingPricingTemplate(null);
    setPricingTemplateImpact(null);
    setPricingTemplateImpactError(null);
    setPricingTemplatePreparingEditId(null);
    setPricingTemplateServerError(null);
    setPricingTemplateDialogOpen(true);
  };

  const handleEditPricingTemplate = async (
    templateSummary: PricingTemplate,
  ) => {
    const generation = ++editGeneration.current;
    const messages = getStaticMessages();
    setPricingTemplatePreparingEditId(templateSummary.id);
    setPricingTemplateImpact(null);
    setPricingTemplateImpactError(null);
    setPricingTemplateImpactLoading(true);
    try {
      const template = await api.pricingTemplates.get(templateSummary.id);
      if (generation !== editGeneration.current) return;
      setEditingPricingTemplate(template);
      setPricingTemplateServerError(null);
      setPricingTemplateDialogOpen(true);
      try {
        const impact = await api.pricingTemplates.impact(templateSummary.id);
        if (generation === editGeneration.current) {
          setPricingTemplateImpact(impact);
        }
      } catch (error) {
        if (generation === editGeneration.current) {
          setPricingTemplateImpactError(
            error instanceof Error
              ? error.message
              : messages.pricingTemplatesData.impactLoadFailed,
          );
        }
      }
    } catch (error) {
      if (generation === editGeneration.current) {
        toast.error(
          error instanceof Error
            ? error.message
            : messages.pricingTemplatesData.loadSingleFailed,
        );
      }
    } finally {
      if (generation === editGeneration.current) {
        setPricingTemplateImpactLoading(false);
        setPricingTemplatePreparingEditId(null);
      }
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
        setPricingTemplateServerError({
          issues: [],
          summary: messages.pricingTemplatesData.changedWhileEditing,
        });
        await fetchPricingTemplates();
        return;
      }
      setPricingTemplateServerError(
        extractServerValidation(error, messages.pricingTemplatesData.saveFailed),
      );
    } finally {
      setPricingTemplateSaving(false);
    }
  };

  const handleDeletePricingTemplateClick = async (
    template: PricingTemplate,
  ) => {
    setDeletePricingTemplateConfirm(template);
    setDeletePricingTemplateConflict(null);
    await loadPricingTemplateUsageForDelete(template);
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

  const retryPricingTemplateImpact = async () => {
    if (!editingPricingTemplate) return;
    const messages = getStaticMessages();
    setPricingTemplateImpactLoading(true);
    setPricingTemplateImpactError(null);
    try {
      setPricingTemplateImpact(
        await api.pricingTemplates.impact(editingPricingTemplate.id),
      );
    } catch (error) {
      setPricingTemplateImpactError(
        error instanceof Error
          ? error.message
          : messages.pricingTemplatesData.impactLoadFailed,
      );
    } finally {
      setPricingTemplateImpactLoading(false);
    }
  };

  return {
    closePricingTemplateDialog,
    deletePricingTemplateConflict,
    deletePricingTemplateConfirm,
    deletePricingTemplateDisplay,
    editingPricingTemplate,
    handleDeletePricingTemplate,
    handleDeletePricingTemplateClick,
    handleEditPricingTemplate,
    handleSavePricingTemplate,
    openCreatePricingTemplateDialog,
    pricingTemplateDeleting,
    pricingTemplateDialogOpen,
    pricingTemplateImpact,
    pricingTemplateImpactError,
    pricingTemplateImpactLoading,
    pricingTemplatePreparingEditId,
    pricingTemplateSaving,
    pricingTemplateServerError,
    retryPricingTemplateImpact,
    setDeletePricingTemplateConfirm,
    setDeletePricingTemplateConflict,
    setPricingTemplateDialogOpen,
  };
}
