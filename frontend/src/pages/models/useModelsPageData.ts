import { useCallback, useEffect, useRef, useState } from "react";
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
    case "openai_image_operations_invalid":
      return messages.modelsData.openaiImageOperationsInvalid;
    case "openai_capability_required":
      return messages.modelsData.openaiCapabilityRequired;
    default:
      return null;
  }
}

function getTrimmedString(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function getModelSaveErrorMessage(error: unknown, fallback: string) {
  if (
    error instanceof ApiError &&
    error.detail &&
    typeof error.detail === "object"
  ) {
    const detail = error.detail as {
      code?: unknown;
      detail?: unknown;
      field?: unknown;
      message?: unknown;
      routing_plan_issues?: unknown;
    };

    if (Array.isArray(detail.routing_plan_issues)) {
      const routingPlanIssue = detail.routing_plan_issues.find(
        (
          issue,
        ): issue is {
          code?: unknown;
          field?: unknown;
          message?: unknown;
          path?: unknown;
        } => !!issue && typeof issue === "object",
      );

      if (routingPlanIssue) {
        const code = getTrimmedString(routingPlanIssue.code);
        const field =
          getTrimmedString(routingPlanIssue.field) ||
          getTrimmedString(routingPlanIssue.path);
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

    const structuredDetail =
      detail.detail && typeof detail.detail === "object"
        ? (detail.detail as {
            code?: unknown;
            field?: unknown;
            message?: unknown;
          })
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
    const updatedAtDelta =
      new Date(right.updated_at).getTime() -
      new Date(left.updated_at).getTime();
    return updatedAtDelta !== 0 ? updatedAtDelta : right.id - left.id;
  });
}

type ModelDialogSession =
  | { readonly mode: "closed" | "edit"; readonly createSession: null }
  | { readonly mode: "create"; readonly createSession: number };

export function useModelsPageData(
  revision: number,
  scope: "ingress" | "final_execution" | "route_attempt" = "ingress",
) {
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<
    LoadbalanceStrategy[]
  >([]);
  const [models, setModels] = useState<ManagedModelConfigListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editingModel, setEditingModel] =
    useState<ManagedModelConfigListItem | null>(null);
  const [deleteTarget, setDeleteTarget] =
    useState<ManagedModelConfigListItem | null>(null);
  const [formData, setFormData] = useState<ModelFormData>(
    DEFAULT_MODEL_FORM_DATA,
  );
  const [formError, setFormError] = useState<string | null>(null);
  const [
    loadbalanceStrategyDefaultsCreating,
    setLoadbalanceStrategyDefaultsCreating,
  ] = useState(false);
  const [togglingModelIds, setTogglingModelIds] = useState<Set<number>>(
    new Set(),
  );
  const modelDialogSessionRef = useRef<ModelDialogSession>({
    mode: "closed",
    createSession: null,
  });
  const nextCreateDialogSessionRef = useRef(0);
  const {
    coverage: metricsCoverage,
    metricsFailed,
    metricsLoading,
    modelMetricsByScope,
  } = useModelMetrics24h(models);
  const modelMetrics24h = Object.fromEntries(
    models.map((model) => [model.id, modelMetricsByScope[model.id]?.[scope]]),
  );
  const modelSpend30dMicros = Object.fromEntries(
    models.map((model) => [
      model.id,
      modelMetricsByScope[model.id]?.[scope]?.known_cost_micros ?? null,
    ]),
  );

  const applyBootstrapData = useCallback(
    (data: {
      loadbalanceStrategiesData: LoadbalanceStrategy[];
      modelsData: ManagedModelConfigListItem[];
    }) => {
      setLoadbalanceStrategies(data.loadbalanceStrategiesData);
      setModels(data.modelsData);
    },
    [],
  );

  const fetchData = useCallback(async (currentRevision: number) => {
    return Promise.all([
      getSharedLoadbalanceStrategies(currentRevision),
      getSharedModels(currentRevision),
    ]).then(([loadbalanceStrategiesData, modelsData]) => ({
      loadbalanceStrategiesData,
      modelsData: modelsData as ManagedModelConfigListItem[],
    }));
  }, []);

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    setLoadError(null);
    void (async () => {
      try {
        const data = await fetchData(revision);
        if (cancelled) return;
        applyBootstrapData(data);
        setLoadError(null);
      } catch (error) {
        if (!cancelled) {
          const message =
            error instanceof Error
              ? error.message
              : getStaticMessages().modelsData.fetchFailed;
          setLoadError(message);
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
  }, [applyBootstrapData, fetchData, loadAttempt, revision]);

  const retryLoad = useCallback(() => {
    setLoadAttempt((current: number) => current + 1);
  }, []);

  const commitModels = (
    updater: (
      current: ManagedModelConfigListItem[],
    ) => ManagedModelConfigListItem[],
  ) => {
    setModels((current: ManagedModelConfigListItem[]) => {
      const next = updater(current);
      setSharedModels(revision, next);
      return next;
    });
  };

  const handleSetIsDialogOpen = (open: boolean) => {
    if (!open) {
      modelDialogSessionRef.current = { mode: "closed", createSession: null };
      // A defaults mutation may have completed while an edit dialog was
      // open. Reconcile the canonical shared list when the modal closes so a
      // later create session sees the authoritative rows, without changing a
      // live edit dialog underneath the operator.
      void getSharedLoadbalanceStrategies(revision).then((strategies) => {
        setLoadbalanceStrategies(sortStrategies(strategies));
      });
    }
    setIsDialogOpen(open);
  };

  const handleSetCreateDialogOpen = (open: boolean) => {
    if (open) {
      const createSession = nextCreateDialogSessionRef.current + 1;
      nextCreateDialogSessionRef.current = createSession;
      modelDialogSessionRef.current = { mode: "create", createSession };
    } else {
      modelDialogSessionRef.current = { mode: "closed", createSession: null };
      void getSharedLoadbalanceStrategies(revision).then((strategies) => {
        setLoadbalanceStrategies(sortStrategies(strategies));
      });
    }
    setCreateDialogOpen(open);
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
      const message =
        loadbalanceStrategies.length === 0
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
        const updated = await api.models.update(
          editingModel.id,
          toModelUpdatePayload(formData),
        );
        commitModels((current) =>
          current.map((model) =>
            model.id === editingModel.id
              ? toModelListItem(updated.model, model)
              : model,
          ),
        );
        toast.success(messages.modelsData.updated);
      } else {
        const created = await api.models.create(toModelCreatePayload(formData));
        commitModels((current) => [...current, toModelListItem(created.model)]);
        toast.success(messages.modelsData.created);
      }
      handleSetIsDialogOpen(false);
    } catch (error) {
      const message = getModelSaveErrorMessage(
        error,
        messages.modelsData.saveFailed,
      );
      setFormError(message);
      toast.error(message);
    }
  };

  const handleModelCreated = async (model: ManagedModelConfigListItem) => {
    const messages = getStaticMessages();
    commitModels((current) => [...current, model]);
    toast.success(messages.modelsData.created);
    setCreateDialogOpen(false);
  };

  /**
   * Row-level enable/disable. `ModelConfigUpdate` is all-optional, so this
   * sends only the flag it changes rather than replaying a whole form payload
   * built from possibly stale row data.
   */
  const setModelEnabled = async (
    model: ManagedModelConfigListItem,
    nextEnabled: boolean,
  ) => {
    const messages = getStaticMessages();
    setTogglingModelIds((current: Set<number>) =>
      new Set(current).add(model.id),
    );
    try {
      const updated = await api.models.update(model.id, {
        is_enabled: nextEnabled,
      });
      commitModels((current) =>
        current.map((item) =>
          item.id === model.id ? toModelListItem(updated.model, item) : item,
        ),
      );
      return true;
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.modelsPage.toggleFailed,
      );
      return false;
    } finally {
      setTogglingModelIds((current: Set<number>) => {
        const next = new Set(current);
        next.delete(model.id);
        return next;
      });
    }
  };

  const setModelsEnabled = async (
    targets: ManagedModelConfigListItem[],
    nextEnabled: boolean,
  ) => {
    const messages = getStaticMessages();
    const results = await Promise.all(
      targets.map((model) => setModelEnabled(model, nextEnabled)),
    );
    const succeeded = results.filter(Boolean).length;
    const failed = results.length - succeeded;
    // Report both halves: a partial batch that only reported success would
    // leave the operator believing rows changed that did not.
    toast.success(
      messages.modelsPage.bulkDone(String(succeeded), String(failed)),
    );
  };

  const handleDelete = async () => {
    const messages = getStaticMessages();
    if (!deleteTarget) return;
    try {
      await api.models.delete(deleteTarget.id);
      commitModels((current) =>
        current.filter((model) => model.id !== deleteTarget.id),
      );
      toast.success(messages.modelsData.deleted);
      setDeleteTarget(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.modelsData.deleteFailed,
      );
    }
  };

  const handleCreateLoadbalanceStrategyDefaults = async () => {
    const messages = getStaticMessages();
    setLoadbalanceStrategyDefaultsCreating(true);
    try {
      const response = await api.loadbalanceStrategies.createDefaults();
      // The mutation response is a canonical-name/id summary, not a full
      // strategy row. Re-read the owner list before exposing options; mapping
      // the summary onto an empty/stale local list would silently leave the
      // create form in an unknown state.
      const next = sortStrategies(
        await getSharedLoadbalanceStrategies(revision, true),
      );
      setSharedLoadbalanceStrategies(revision, next);
      const currentDialog = modelDialogSessionRef.current;
      if (currentDialog.mode === "create") {
        setLoadbalanceStrategies(next);
        setFormData((current: ModelFormData) =>
          setLoadbalanceStrategyIdOnForm(current, next[0]?.id ?? null),
        );
      } else if (currentDialog.mode === "closed") {
        setLoadbalanceStrategies(next);
      }
      toast.success(
        response.created.length > 0
          ? messages.loadbalanceStrategiesData.defaultsCreated
          : messages.loadbalanceStrategiesData.defaultsAlreadyExisted,
      );
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.loadbalanceStrategiesData.saveFailed,
      );
    } finally {
      setLoadbalanceStrategyDefaultsCreating(false);
    }
  };

  const setLoadbalanceStrategyId = (value: number | null) => {
    setFormData((current: ModelFormData) =>
      setLoadbalanceStrategyIdOnForm(current, value),
    );
  };

  return {
    deleteTarget,
    editingModel,
    formData,
    formError,
    handleDelete,
    handleModelCreated,
    createDialogOpen,
    setCreateDialogOpen: handleSetCreateDialogOpen,
    handleCreateLoadbalanceStrategyDefaults,
    handleOpenDialog,
    handleSubmit,
    isDialogOpen,
    loadbalanceStrategies,
    loadbalanceStrategyDefaultsCreating,
    loading,
    loadError,
    metricsFailed,
    metricsCoverage,
    metricsLoading,
    modelMetricsByScope,
    modelMetrics24h,
    modelSpend30dMicros,
    models,
    setDeleteTarget,
    setModelEnabled,
    setModelsEnabled,
    togglingModelIds,
    setFormData,
    setIsDialogOpen: handleSetIsDialogOpen,
    setLoadbalanceStrategyId,
    retryLoad,
  };
}
