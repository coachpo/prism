import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { ManagedModelConfigListItem } from "@/lib/api/management";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedLoadbalanceStrategies,
  getSharedModels,
  setSharedLoadbalanceStrategies,
  setSharedModels,
} from "@/lib/referenceData";
import type { LoadbalanceStrategy } from "@/lib/types";
import type { ModelCreatePayloadWithTarget } from "./createModelDialogPayload";
import { toast } from "sonner";
import {
  createEditModelFormData,
  createNewModelFormData,
  DEFAULT_MODEL_FORM_DATA,
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
    case "openai_accepted_format_invalid":
      return messages.modelsData.openaiAcceptedFormatInvalid;
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

function sortStrategies(strategies: LoadbalanceStrategy[]) {
  return [...strategies].sort((left, right) => {
    const updatedAtDelta = new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime();
    return updatedAtDelta !== 0 ? updatedAtDelta : right.id - left.id;
  });
}

type ModelDialogSession =
  | { readonly mode: "closed" | "edit"; readonly createSession: null }
  | { readonly mode: "create"; readonly createSession: number };

export function useModelsPageData(revision: number) {
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([]);
  const [models, setModels] = useState<ManagedModelConfigListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<ManagedModelConfigListItem | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ManagedModelConfigListItem | null>(null);
  const [search, setSearch] = useState("");
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [formError, setFormError] = useState<string | null>(null);
  const [loadbalanceStrategyDefaultsCreating, setLoadbalanceStrategyDefaultsCreating] = useState(false);
  const modelDialogSessionRef = useRef<ModelDialogSession>({ mode: "closed", createSession: null });
  const nextCreateDialogSessionRef = useRef(0);
  const { metricsLoading, modelMetrics24h, modelSpend30dMicros } = useModelMetrics24h(models);

  const applyBootstrapData = useCallback((data: {
    loadbalanceStrategiesData: LoadbalanceStrategy[];
    modelsData: ManagedModelConfigListItem[];
  }) => {
    setLoadbalanceStrategies(data.loadbalanceStrategiesData);
    setModels(data.modelsData);
  }, []);

  const fetchData = useCallback(async (currentRevision: number) => {
    return Promise.all([
      getSharedLoadbalanceStrategies(currentRevision),
      getSharedModels(currentRevision),
    ]).then(
      ([loadbalanceStrategiesData, modelsData]) => ({
        loadbalanceStrategiesData,
        modelsData: modelsData as ManagedModelConfigListItem[],
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

  const handleSetIsDialogOpen = (open: boolean) => {
    if (!open) {
      modelDialogSessionRef.current = { mode: "closed", createSession: null };
    }
    setIsDialogOpen(open);
  };

  const handleOpenDialog = async (model?: ManagedModelConfigListItem) => {
    if (model) {
      modelDialogSessionRef.current = { mode: "edit", createSession: null };
      setEditingModel(model);
      setFormData(createEditModelFormData(model));
      setFormError(null);
      setIsDialogOpen(true);
      return;
    }

    const createSession = nextCreateDialogSessionRef.current + 1;
    nextCreateDialogSessionRef.current = createSession;
    modelDialogSessionRef.current = { mode: "create", createSession };
    setEditingModel(null);
    setFormData(createNewModelFormData(loadbalanceStrategies[0]?.id ?? null));
    setFormError(null);
    setIsDialogOpen(true);
  };

  const handleSubmit = async (event: SubmitEventLike) => {
    const messages = getStaticMessages();
    event.preventDefault();
    setFormError(null);
    const validationError = validateModelFormData(formData);

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

    if (validationError) {
      const message = getModelValidationMessage(messages, validationError);
      if (message) {
        setFormError(message);
        return;
      }
    }

    try {
      if (editingModel) {
        const updatedResponse = await api.models.update(editingModel.id, toModelUpdatePayload(formData));
        commitModels((current) =>
          current.map((model) =>
            model.id === editingModel.id ? toModelListItem(updatedResponse.model, model) : model
          )
        );
        toast.success(messages.modelsData.updated);
      } else {
        const createdResponse = await api.models.create(toModelCreatePayload(formData));
        commitModels((current) => [...current, toModelListItem(createdResponse.model)]);
        toast.success(messages.modelsData.created);
      }
      handleSetIsDialogOpen(false);
    } catch (error) {
      const message = getModelSaveErrorMessage(error, messages.modelsData.saveFailed);
      setFormError(message);
      toast.error(message);
    }
  };

  const handleCreateModelSubmit = async (payload: ModelCreatePayloadWithTarget) => {
    const messages = getStaticMessages();
    const createdResponse = await api.models.create(payload);
    commitModels((current) => [...current, toModelListItem(createdResponse.model)]);
    toast.success(messages.modelsData.created);
    return createdResponse;
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

  const handleCreateLoadbalanceStrategyDefaults = async () => {
    const messages = getStaticMessages();
    const createSession = modelDialogSessionRef.current.mode === "create"
      ? modelDialogSessionRef.current.createSession
      : null;
    setLoadbalanceStrategyDefaultsCreating(true);
    try {
      const response = await api.loadbalanceStrategies.createDefaults();
      const next = sortStrategies(response.items);
      setLoadbalanceStrategies(next);
      setSharedLoadbalanceStrategies(revision, next);
      if (modelDialogSessionRef.current.mode === "create" && modelDialogSessionRef.current.createSession === createSession) {
        setFormData((current) => setLoadbalanceStrategyIdOnForm(current, next[0]?.id ?? null));
      }
      toast.success(response.created_count > 0 ? messages.loadbalanceStrategiesData.defaultsCreated : messages.loadbalanceStrategiesData.defaultsAlreadyExisted);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.loadbalanceStrategiesData.saveFailed);
    } finally {
      setLoadbalanceStrategyDefaultsCreating(false);
    }
  };

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
    handleCreateLoadbalanceStrategyDefaults,
    handleOpenDialog,
    handleSubmit,
    handleCreateModelSubmit,
    isDialogOpen,
    loadbalanceStrategies,
    loadbalanceStrategyDefaultsCreating,
    loading,
    metricsLoading,
    modelMetrics24h,
    modelSpend30dMicros,
    models,
    search,
    setDeleteTarget,
    setFormData,
    setIsDialogOpen: handleSetIsDialogOpen,
    setLoadbalanceStrategyId,
    setSearch,
  };
}
