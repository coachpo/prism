import { useCallback, useRef, useState } from "react";

import { ApiError, api } from "@/lib/api";
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import { getStaticMessages } from "@/i18n/staticMessages";
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
  toModelUpdatePayload,
  type SubmitEventLike,
  validateModelFormData,
} from "./modelFormState";
import { toModelListItem } from "./modelListProjection";

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

        if (message && field && code) return `${field} (${code}): ${message}`;
        if (message && code) return `${code}: ${message}`;
        if (message) return message;
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

    if (message && field && code) return `${field} (${code}): ${message}`;
    if (message && code) return `${code}: ${message}`;
    if (message) return message;
  }

  return error instanceof Error ? error.message : fallback;
}

export type ModelDialogSession =
  | { readonly mode: "closed" | "edit"; readonly createSession: null }
  | { readonly mode: "create"; readonly createSession: number };

interface UseModelDialogMutationsInput {
  commitModels: (
    updater: (
      current: ManagedModelConfigListItem[],
    ) => ManagedModelConfigListItem[],
  ) => void;
  loadbalanceStrategies: LoadbalanceStrategy[];
  refreshStrategiesAfterDialogClose: () => void;
}

export function useModelDialogMutations({
  commitModels,
  loadbalanceStrategies,
  refreshStrategiesAfterDialogClose,
}: UseModelDialogMutationsInput) {
  const [isDialogOpen, setIsDialogOpenState] = useState(false);
  const [createDialogOpen, setCreateDialogOpenState] = useState(false);
  const [editingModel, setEditingModel] =
    useState<ManagedModelConfigListItem | null>(null);
  const [formData, setFormData] =
    useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [formError, setFormError] = useState<string | null>(null);
  const modelDialogSessionRef = useRef<ModelDialogSession>({
    mode: "closed",
    createSession: null,
  });
  const nextCreateDialogSessionRef = useRef(0);

  const handleSetIsDialogOpen = useCallback(
    (open: boolean) => {
      if (!open) {
        modelDialogSessionRef.current = {
          mode: "closed",
          createSession: null,
        };
        refreshStrategiesAfterDialogClose();
      }
      setIsDialogOpenState(open);
    },
    [refreshStrategiesAfterDialogClose],
  );

  const handleSetCreateDialogOpen = useCallback(
    (open: boolean) => {
      if (open) {
        const createSession = nextCreateDialogSessionRef.current + 1;
        nextCreateDialogSessionRef.current = createSession;
        modelDialogSessionRef.current = { mode: "create", createSession };
      } else {
        modelDialogSessionRef.current = {
          mode: "closed",
          createSession: null,
        };
        refreshStrategiesAfterDialogClose();
      }
      setCreateDialogOpenState(open);
    },
    [refreshStrategiesAfterDialogClose],
  );

  const handleOpenDialog = useCallback(
    async (model?: ManagedModelConfigListItem) => {
      if (model) {
        modelDialogSessionRef.current = {
          mode: "edit",
          createSession: null,
        };
        setEditingModel(model);
        setFormData(createEditModelFormData(model));
        setFormError(null);
        setIsDialogOpenState(true);
        return;
      }

      const createSession = nextCreateDialogSessionRef.current + 1;
      nextCreateDialogSessionRef.current = createSession;
      modelDialogSessionRef.current = { mode: "create", createSession };
      setEditingModel(null);
      setFormData(createNewModelFormData(loadbalanceStrategies[0]?.id ?? null));
      setFormError(null);
      setIsDialogOpenState(true);
    },
    [loadbalanceStrategies],
  );

  const handleSubmit = useCallback(
    async (event: SubmitEventLike) => {
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
          const created = await api.models.create(
            toModelCreatePayload(formData),
          );
          commitModels((current) => [
            ...current,
            toModelListItem(created.model),
          ]);
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
    },
    [
      commitModels,
      editingModel,
      formData,
      handleSetIsDialogOpen,
      loadbalanceStrategies.length,
    ],
  );

  const handleModelCreated = useCallback(
    async (model: ManagedModelConfigListItem) => {
      const messages = getStaticMessages();
      commitModels((current) => [...current, model]);
      toast.success(messages.modelsData.created);
      // Preserve the composite create dialog's existing close/session path.
      setCreateDialogOpenState(false);
    },
    [commitModels],
  );

  const setLoadbalanceStrategyId = useCallback((value: number | null) => {
    setFormData((current) => setLoadbalanceStrategyIdOnForm(current, value));
  }, []);

  return {
    createDialogOpen,
    editingModel,
    formData,
    formError,
    handleModelCreated,
    handleOpenDialog,
    handleSetCreateDialogOpen,
    handleSetIsDialogOpen,
    handleSubmit,
    isDialogOpen,
    modelDialogSessionRef,
    setFormData,
    setLoadbalanceStrategyId,
  };
}
