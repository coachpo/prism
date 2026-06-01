import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedLoadbalanceStrategies,
  getSharedModels,
  getSharedVendors,
  setSharedModels,
} from "@/lib/referenceData";
import type {
  LoadbalanceStrategy,
  ModelConfigListItem,
  Vendor,
} from "@/lib/types";
import { toast } from "sonner";
import {
  createEditModelFormData,
  createNewModelFormData,
  DEFAULT_MODEL_FORM_DATA,
  getAccessTargetModelsForApiFamily,
  getAccessTargetOptionKeys,
  type ModelFormData,
  setLoadbalanceStrategyIdOnForm,
  toModelCreatePayload,
  toModelListItem,
  toModelUpdatePayload,
  type SubmitEventLike,
  validateModelFormData,
} from "./modelFormState";
import { useModelMetrics24h } from "./useModelMetrics24h";

export function useModelsPageData(revision: number) {
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([]);
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [vendors, setVendors] = useState<Vendor[]>([]);
  const [loading, setLoading] = useState(true);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ModelConfigListItem | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ModelConfigListItem | null>(null);
  const [search, setSearch] = useState("");
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [formError, setFormError] = useState<string | null>(null);
  const { metricsLoading, modelMetrics24h, modelSpend30dMicros } = useModelMetrics24h(models);

  const applyBootstrapData = useCallback((data: {
    loadbalanceStrategiesData: LoadbalanceStrategy[];
    modelsData: ModelConfigListItem[];
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
        modelsData,
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

  const commitModels = (updater: (current: ModelConfigListItem[]) => ModelConfigListItem[]) => {
    setModels((current) => {
      const next = updater(current);
      setSharedModels(revision, next);
      return next;
    });
  };

  const handleOpenDialog = async (model?: ModelConfigListItem) => {
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
      const message = error instanceof Error ? error.message : messages.modelsData.saveFailed;
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
