import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { isValidCurrencyCode } from "@/lib/costing";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedPricingTemplates,
  setSharedPricingTemplates,
} from "@/lib/referenceData";
import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateCreate,
  PricingTemplateUpdate,
} from "@/lib/types";
import { toast } from "sonner";
import {
  DEFAULT_PRICING_TEMPLATE_FORM,
  isNonNegativeDecimalString,
  normalizeOptionalTemplatePrice,
  parsePricingTemplateUsageRows,
  pricingTemplateFormStateFromTemplate,
  type PricingTemplateFormState,
} from "./pricingTemplateFormState";

export function usePricingTemplatesPageData(revision: number) {
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([]);
  const [pricingTemplatesLoading, setPricingTemplatesLoading] = useState(false);
  const [pricingTemplateDialogOpen, setPricingTemplateDialogOpen] = useState(false);
  const [editingPricingTemplate, setEditingPricingTemplate] = useState<PricingTemplate | null>(null);
  const [pricingTemplatePreparingEditId, setPricingTemplatePreparingEditId] = useState<number | null>(null);
  const [pricingTemplateForm, setPricingTemplateForm] = useState<PricingTemplateFormState>(DEFAULT_PRICING_TEMPLATE_FORM);
  const [pricingTemplateSaving, setPricingTemplateSaving] = useState(false);
  const [pricingTemplateUsageDialogOpen, setPricingTemplateUsageDialogOpen] = useState(false);
  const [pricingTemplateUsageRows, setPricingTemplateUsageRows] = useState<PricingTemplateConnectionUsageItem[]>([]);
  const [pricingTemplateUsageLoading, setPricingTemplateUsageLoading] = useState(false);
  const [pricingTemplateUsageTemplate, setPricingTemplateUsageTemplate] = useState<PricingTemplate | null>(null);
  const [deletePricingTemplateConfirm, setDeletePricingTemplateConfirm] = useState<PricingTemplate | null>(null);
  const [deletePricingTemplateConflict, setDeletePricingTemplateConflict] = useState<PricingTemplateConnectionUsageItem[] | null>(null);
  const [pricingTemplateDeleting, setPricingTemplateDeleting] = useState(false);

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

  const fetchPricingTemplates = useCallback(async () => {
    const messages = getStaticMessages();
    setPricingTemplatesLoading(true);
    try {
      const data = await getSharedPricingTemplates(revision);
      setPricingTemplates(sortPricingTemplates(data));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadFailed);
    } finally {
      setPricingTemplatesLoading(false);
    }
  }, [revision]);

  useEffect(() => {
    void revision;
    void fetchPricingTemplates();
  }, [fetchPricingTemplates, revision]);

  const closePricingTemplateDialog = () => {
    setPricingTemplateDialogOpen(false);
  };

  const openCreatePricingTemplateDialog = () => {
    setEditingPricingTemplate(null);
    setPricingTemplatePreparingEditId(null);
    setPricingTemplateForm(DEFAULT_PRICING_TEMPLATE_FORM);
    setPricingTemplateDialogOpen(true);
  };

  const handleEditPricingTemplate = async (templateSummary: PricingTemplate) => {
    const messages = getStaticMessages();
    setPricingTemplatePreparingEditId(templateSummary.id);
    try {
      const template = await api.pricingTemplates.get(templateSummary.id);
      setEditingPricingTemplate(template);
      setPricingTemplateForm(pricingTemplateFormStateFromTemplate(template));
      setPricingTemplateDialogOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadSingleFailed);
    } finally {
      setPricingTemplatePreparingEditId(null);
    }
  };

  const handleSavePricingTemplate = async () => {
    const messages = getStaticMessages();
    if (!pricingTemplateForm.name.trim()) {
      toast.error(messages.pricingTemplatesData.nameRequired);
      return;
    }
    if (!isValidCurrencyCode(pricingTemplateForm.pricing_currency_code)) {
      toast.error(messages.pricingTemplatesData.invalidCurrency);
      return;
    }
    if (!isNonNegativeDecimalString(pricingTemplateForm.input_price)) {
      toast.error(messages.pricingTemplatesData.inputNonNegative);
      return;
    }
    if (!isNonNegativeDecimalString(pricingTemplateForm.output_price)) {
      toast.error(messages.pricingTemplatesData.outputNonNegative);
      return;
    }

    const cachedInputPrice = normalizeOptionalTemplatePrice(pricingTemplateForm.cached_input_price);
    if (cachedInputPrice && !isNonNegativeDecimalString(cachedInputPrice)) {
      toast.error(messages.pricingTemplatesData.cachedInputNonNegative);
      return;
    }

    const cacheCreationPrice = normalizeOptionalTemplatePrice(pricingTemplateForm.cache_creation_price);
    if (cacheCreationPrice && !isNonNegativeDecimalString(cacheCreationPrice)) {
      toast.error(messages.pricingTemplatesData.cacheCreationNonNegative);
      return;
    }

    const reasoningPrice = normalizeOptionalTemplatePrice(pricingTemplateForm.reasoning_price);
    if (reasoningPrice && !isNonNegativeDecimalString(reasoningPrice)) {
      toast.error(messages.pricingTemplatesData.reasoningNonNegative);
      return;
    }

    setPricingTemplateSaving(true);
    try {
      if (editingPricingTemplate) {
        const payload: PricingTemplateUpdate = {
          expected_updated_at: editingPricingTemplate.updated_at,
          name: pricingTemplateForm.name.trim(),
          description: pricingTemplateForm.description.trim() || null,
          pricing_currency_code: pricingTemplateForm.pricing_currency_code.trim().toUpperCase(),
          input_price: pricingTemplateForm.input_price.trim(),
          output_price: pricingTemplateForm.output_price.trim(),
          cached_input_price: cachedInputPrice,
          cache_creation_price: cacheCreationPrice,
          reasoning_price: reasoningPrice,
          missing_special_token_price_policy: pricingTemplateForm.missing_special_token_price_policy,
        };
        const updated = await api.pricingTemplates.update(editingPricingTemplate.id, payload);
        commitPricingTemplates((current) =>
          current.map((template) =>
            template.id === editingPricingTemplate.id ? updated : template
          )
        );
        toast.success(messages.pricingTemplatesData.updated);
      } else {
        const payload: PricingTemplateCreate = {
          name: pricingTemplateForm.name.trim(),
          description: pricingTemplateForm.description.trim() || null,
          pricing_currency_code: pricingTemplateForm.pricing_currency_code.trim().toUpperCase(),
          input_price: pricingTemplateForm.input_price.trim(),
          output_price: pricingTemplateForm.output_price.trim(),
          cached_input_price: cachedInputPrice,
          cache_creation_price: cacheCreationPrice,
          reasoning_price: reasoningPrice,
          missing_special_token_price_policy: pricingTemplateForm.missing_special_token_price_policy,
        };
        const created = await api.pricingTemplates.create(payload);
        commitPricingTemplates((current) => [created, ...current]);
        toast.success(messages.pricingTemplatesData.created);
      }

      closePricingTemplateDialog();
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
          toast.error(messages.pricingTemplatesData.changedWhileEditing);
        await fetchPricingTemplates();
        return;
      }
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.saveFailed);
    } finally {
      setPricingTemplateSaving(false);
    }
  };

  const handleViewPricingTemplateUsage = async (template: PricingTemplate) => {
    const messages = getStaticMessages();
    setPricingTemplateUsageTemplate(template);
    setPricingTemplateUsageDialogOpen(true);
    setPricingTemplateUsageLoading(true);
    try {
      const data = await api.pricingTemplates.connections(template.id);
      setPricingTemplateUsageRows(data.items);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadUsageFailed);
      setPricingTemplateUsageRows([]);
    } finally {
      setPricingTemplateUsageLoading(false);
    }
  };

  const handleDeletePricingTemplateClick = async (template: PricingTemplate) => {
    const messages = getStaticMessages();
    setDeletePricingTemplateConfirm(template);
    setDeletePricingTemplateConflict(null);
    setPricingTemplateUsageLoading(true);
    try {
      const data = await api.pricingTemplates.connections(template.id);
      setPricingTemplateUsageRows(data.items);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadUsageFailed);
      setPricingTemplateUsageRows([]);
    } finally {
      setPricingTemplateUsageLoading(false);
    }
  };

  const handleDeletePricingTemplate = async () => {
    const messages = getStaticMessages();
    if (!deletePricingTemplateConfirm) {
      return;
    }

    setPricingTemplateDeleting(true);
    try {
      await api.pricingTemplates.delete(deletePricingTemplateConfirm.id);
      commitPricingTemplates((current) =>
        current.filter((template) => template.id !== deletePricingTemplateConfirm.id)
      );
      toast.success(messages.pricingTemplatesData.deleted);
      setDeletePricingTemplateConfirm(null);
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const conflictRows = parsePricingTemplateUsageRows(error.detail);
        setDeletePricingTemplateConflict(conflictRows);
        toast.error(messages.pricingTemplatesData.inUseCannotDelete);
      } else {
        toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.deleteFailed);
      }
    } finally {
      setPricingTemplateDeleting(false);
    }
  };

  return {
    closePricingTemplateDialog,
    deletePricingTemplateConfirm,
    deletePricingTemplateConflict,
    editingPricingTemplate,
    handleDeletePricingTemplate,
    handleDeletePricingTemplateClick,
    handleEditPricingTemplate,
    handleSavePricingTemplate,
    handleViewPricingTemplateUsage,
    openCreatePricingTemplateDialog,
    pricingTemplateDeleting,
    pricingTemplateDialogOpen,
    pricingTemplateForm,
    pricingTemplatePreparingEditId,
    pricingTemplateSaving,
    pricingTemplateUsageDialogOpen,
    pricingTemplateUsageLoading,
    pricingTemplateUsageRows,
    pricingTemplateUsageTemplate,
    pricingTemplates,
    pricingTemplatesLoading,
    setDeletePricingTemplateConfirm,
    setDeletePricingTemplateConflict,
    setPricingTemplateDialogOpen,
    setPricingTemplateForm,
    setPricingTemplateUsageDialogOpen,
  };
}

function sortPricingTemplates(templates: PricingTemplate[]) {
  return [...templates].sort((left, right) => {
    const updatedAtDelta =
      new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime();

    if (updatedAtDelta !== 0) {
      return updatedAtDelta;
    }

    return right.id - left.id;
  });
}
