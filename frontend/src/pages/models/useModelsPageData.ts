import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { ManagedModelConfigListItem } from "@/lib/api/management";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedLoadbalanceStrategies,
  getSharedModels,
  getSharedVendors,
  setSharedModels,
} from "@/lib/referenceData";
import type {
  LoadbalanceStrategy,
  Vendor,
} from "@/lib/types";
import { toast } from "sonner";
import {
  createEditModelFormData,
  createNewModelFormData,
  DEFAULT_MODEL_FORM_DATA,
  getAccessTargetModelsForApiFamily,
  getAccessTargetOptionKeys,
  getPromotionTargetModelsForApiFamily,
  type ModelFormData,
  type ModelFormValidationError,
  setLoadbalanceStrategyIdOnForm,
  toModelCreatePayload,
  toModelListItem,
  toModelUpdatePayload,
  type SubmitEventLike,
  validateModelFormData,
} from "./modelFormState";
import { useModelMetrics24h } from "./useModelMetrics24h";

function getModelValidationMessage(
  messages: ReturnType<typeof getStaticMessages>,
  validationError: ModelFormValidationError,
): string | null {
  switch (validationError) {
    case "model_id_required":
      return messages.modelsData.modelIdRequired;
    case "context_window_tokens_invalid":
      return messages.modelsData.contextWindowTokensInvalid;
    case "default_output_token_reserve_invalid":
      return messages.modelsData.defaultOutputTokenReserveInvalid;
    case "max_context_utilization_invalid":
      return messages.modelsData.maxContextUtilizationInvalid;
    case "preferred_context_utilization_threshold_invalid":
      return messages.modelsData.preferredContextUtilizationThresholdInvalid;
    case "preferred_context_utilization_threshold_exceeds_max":
      return messages.modelsData.preferredContextUtilizationThresholdExceedsMaxContextUtilization;
    default:
      return null;
  }
}

function getTrimmedString(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function getModelSaveErrorMessage(error: unknown, fallback: string) {
  if (error instanceof ApiError && error.detail && typeof error.detail === "object") {
    const detail = error.detail as {
      code?: unknown;
      detail?: unknown;
      field?: unknown;
      message?: unknown;
      routing_plan_issues?: unknown;
    };

    if (Array.isArray(detail.routing_plan_issues)) {
      const routingPlanIssue = detail.routing_plan_issues.find(
        (issue): issue is { code?: unknown; field?: unknown; message?: unknown; path?: unknown } =>
          !!issue && typeof issue === "object",
      );

      if (routingPlanIssue) {
        const code = getTrimmedString(routingPlanIssue.code);
        const field = getTrimmedString(routingPlanIssue.field) || getTrimmedString(routingPlanIssue.path);
        const message = getTrimmedString(routingPlanIssue.message);

        if (message && field && code) {
          return `${field} (${code}): ${message}`;
        }
        if (message && code) {
          return `${code}: ${message}`;
        }
        if (message) {
          return message;
        }
      }
    }

    const structuredDetail = detail.detail && typeof detail.detail === "object"
      ? detail.detail as {
          code?: unknown;
          field?: unknown;
          message?: unknown;
        }
      : detail;
    const code = getTrimmedString(structuredDetail.code);
    const field = getTrimmedString(structuredDetail.field);
    const message = getTrimmedString(structuredDetail.message);

    if (message && field && code) {
      return `${field} (${code}): ${message}`;
    }
    if (message && code) {
      return `${code}: ${message}`;
    }
    if (message) {
      return message;
    }
  }

  return error instanceof Error ? error.message : fallback;
}

export function useModelsPageData(revision: number) {
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([]);
  const [models, setModels] = useState<ManagedModelConfigListItem[]>([]);
  const [vendors, setVendors] = useState<Vendor[]>([]);
  const [loading, setLoading] = useState(true);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ManagedModelConfigListItem | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ManagedModelConfigListItem | null>(null);
  const [search, setSearch] = useState("");
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [formError, setFormError] = useState<string | null>(null);
  const { metricsLoading, modelMetrics24h, modelSpend30dMicros } = useModelMetrics24h(models);

  const applyBootstrapData = useCallback((data: {
    loadbalanceStrategiesData: LoadbalanceStrategy[];
    modelsData: ManagedModelConfigListItem[];
    vendorsData: Vendor[];
  }) => {
    setLoadbalanceStrategies(data.loadbalanceStrategiesData);
    setModels(data.modelsData);
    setVendors(data.vendorsData);
  }, []);

  const fetchData = useCallback(async (currentRevision: number) => {
    return Promise.all([
      getSharedLoadbalanceStrategies(currentRevision),
      getSharedModels(currentRevision),
      getSharedVendors(currentRevision),
    ]).then(
      ([loadbalanceStrategiesData, modelsData, vendorsData]) => ({
        loadbalanceStrategiesData,
        modelsData: modelsData as ManagedModelConfigListItem[],
        vendorsData,
      })
    );
  }, []);

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    void (async () => {
      try {
        const data = await fetchData(revision);
        if (cancelled) return;
        applyBootstrapData(data);
      } catch (error) {
        if (!cancelled) {
          toast.error(getStaticMessages().modelsData.fetchFailed);
          console.error(error);
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [applyBootstrapData, fetchData, revision]);

  const commitModels = (updater: (current: ManagedModelConfigListItem[]) => ManagedModelConfigListItem[]) => {
    setModels((current) => {
      const next = updater(current);
      setSharedModels(revision, next);
      return next;
    });
  };

  const handleOpenDialog = async (model?: ManagedModelConfigListItem) => {
    if (model) {
      setEditingModel(model);
      setFormData(createEditModelFormData(model));
      setFormError(null);
      setIsDialogOpen(true);
      return;
    }

    let nextVendors = vendors;
    try {
      nextVendors = await getSharedVendors(revision);
      setVendors(nextVendors);
    } catch {
      nextVendors = vendors;
    }

    setEditingModel(null);
    setFormData(createNewModelFormData(nextVendors, loadbalanceStrategies[0]?.id ?? null));
    setFormError(null);
    setIsDialogOpen(true);
  };

  const handleSubmit = async (event: SubmitEventLike) => {
    const messages = getStaticMessages();
    event.preventDefault();
    setFormError(null);
    const validationError = validateModelFormData(
      formData,
      getAccessTargetOptionKeys(targetModelsForApiFamily),
    );

    if (validationError === "api_family_required") {
      toast.error(messages.modelsData.selectApiFamily);
      return;
    }

    if (validationError === "loadbalance_strategy_required") {
      const message = loadbalanceStrategies.length === 0
        ? messages.modelDetail.noLoadbalanceStrategiesAvailable
        : messages.modelsData.selectLoadbalanceStrategy;
      setFormError(message);
      toast.error(message);
      return;
    }

    if (validationError === "access_target_required") {
      const message = messages.modelsData.enabledAccessTargetRequired;
      setFormError(message);
      toast.error(message);
      return;
    }

    if (validationError) {
      const message = getModelValidationMessage(messages, validationError);
      if (message) {
        setFormError(message);
        return;
      }
    }

    try {
      if (editingModel) {
        const updated = await api.models.update(editingModel.id, toModelUpdatePayload(formData));
        commitModels((current) =>
          current.map((model) =>
            model.id === editingModel.id ? toModelListItem(updated, model) : model
          )
        );
        toast.success(messages.modelsData.updated);
      } else {
        const created = await api.models.create(toModelCreatePayload(formData));
        commitModels((current) => [...current, toModelListItem(created)]);
        toast.success(messages.modelsData.created);
      }
      setIsDialogOpen(false);
    } catch (error) {
      const message = getModelSaveErrorMessage(error, messages.modelsData.saveFailed);
      setFormError(message);
      toast.error(message);
    }
  };

  const handleDelete = async () => {
    const messages = getStaticMessages();
    if (!deleteTarget) return;
    try {
      await api.models.delete(deleteTarget.id);
      commitModels((current) => current.filter((model) => model.id !== deleteTarget.id));
      toast.success(messages.modelsData.deleted);
      setDeleteTarget(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.modelsData.deleteFailed);
    }
  };

  const selectedVendor = vendors.find((vendor) => vendor.id === formData.vendor_id);
  const targetModelsForApiFamily = getAccessTargetModelsForApiFamily(
    models,
    formData.api_family ?? "openai",
    editingModel ? formData.model_id : undefined,
  );
  const promotionTargetModelsForApiFamily = getPromotionTargetModelsForApiFamily(
    models,
    formData.api_family ?? "openai",
  );

  const filtered = useMemo(
    () =>
      models.filter((model) => {
        if (!search) {
          return true;
        }

        const query = search.toLowerCase();
        return (
          model.model_id.toLowerCase().includes(query) ||
          (model.display_name ?? "").toLowerCase().includes(query)
        );
      }),
    [models, search]
  );

  const setLoadbalanceStrategyId = (value: number | null) => {
    setFormData((current) => setLoadbalanceStrategyIdOnForm(current, value));
  };

  return {
    deleteTarget,
    editingModel,
    filtered,
    formData,
    formError,
    handleDelete,
    handleOpenDialog,
    handleSubmit,
    isDialogOpen,
    loadbalanceStrategies,
    loading,
    metricsLoading,
    modelMetrics24h,
    modelSpend30dMicros,
    models,
    promotionTargetModelsForApiFamily,
    targetModelsForApiFamily,
    vendors,
    search,
    selectedVendor,
    setDeleteTarget,
    setFormData,
    setIsDialogOpen,
    setLoadbalanceStrategyId,
    setSearch,
  };
}
