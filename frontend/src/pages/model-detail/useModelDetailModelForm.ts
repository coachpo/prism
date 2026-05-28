import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { Connection, ModelConfig, ModelConfigListItem } from "@/lib/types";
import {
  createEditModelFormData,
  DEFAULT_MODEL_FORM_DATA,
  getAccessTargetModelsForApiFamily,
  getAccessTargetOptionKeys,
  setLoadbalanceStrategyIdOnForm,
  toModelUpdatePayload,
  type ModelFormData,
  type SubmitEventLike,
  validateModelFormData,
} from "../models/modelFormState";
import { patchModelListItemFromDetail } from "./useModelDetailDataSupport";

interface UseModelDetailModelFormInput {
  allConnections: Connection[];
  allModels: ModelConfigListItem[];
  model: ModelConfig | null;
  revision: number;
  setIsEditModelDialogOpenState: (open: boolean) => void;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
}

export function useModelDetailModelForm({
  allConnections,
  allModels,
  model,
  revision,
  setIsEditModelDialogOpenState,
  setAllModels,
  setModel,
}: UseModelDetailModelFormInput) {
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [targetEditorError, setTargetEditorError] = useState<string | null>(null);

  const applyUpdatedModel = useCallback(
    (updatedModel: ModelConfig) => {
      clearSharedReferenceData(undefined, revision);
      setModel(updatedModel);
      setAllModels((currentModels) => patchModelListItemFromDetail(currentModels, updatedModel));
    },
    [revision, setAllModels, setModel],
  );

  const setLoadbalanceStrategyId = useCallback((value: number | null) => {
    setFormData((current) => setLoadbalanceStrategyIdOnForm(current, value));
  }, []);

  const setIsEditModelDialogOpen = useCallback(
    (open: boolean) => {
      if (open) {
        setFormData(model ? createEditModelFormData(model) : DEFAULT_MODEL_FORM_DATA);
        setTargetEditorError(null);
      }

      setIsEditModelDialogOpenState(open);
    },
    [model, setIsEditModelDialogOpenState],
  );

  const handleEditModelSubmit = useCallback(
    async (event: SubmitEventLike) => {
      event.preventDefault();
      if (!model) {
        return;
      }

      const messages = getStaticMessages();
      setTargetEditorError(null);
      const targetModelsForApiFamily = getAccessTargetModelsForApiFamily(
        allModels,
        formData.api_family,
        model.model_id,
      );
      const targetConnectionsForApiFamily = allConnections.filter(
        (connection) => connection.api_family === formData.api_family,
      );
      const validationError = validateModelFormData(
        formData,
        getAccessTargetOptionKeys(targetModelsForApiFamily, targetConnectionsForApiFamily),
      );

      if (validationError === "api_family_required") {
        toast.error(messages.modelDetailData.selectApiFamily);
        return;
      }

      if (validationError === "loadbalance_strategy_required") {
        toast.error(messages.modelDetailData.selectLoadbalanceStrategy);
        return;
      }

      if (validationError === "access_target_required") {
        const message = "Add at least one enabled same-family access target before saving an enabled model.";
        setTargetEditorError(message);
        toast.error(message);
        return;
      }

      try {
        const updatedModel = await api.models.update(model.id, toModelUpdatePayload(formData));
        applyUpdatedModel(updatedModel);
        toast.success(messages.modelDetailData.modelUpdated);
        setIsEditModelDialogOpen(false);
      } catch (error) {
        const message = error instanceof Error ? error.message : messages.modelDetailData.updateModelFailed;
        setTargetEditorError(message);
        toast.error(message);
      }
    },
    [allConnections, allModels, applyUpdatedModel, formData, model, setIsEditModelDialogOpen],
  );

  return {
    formData,
    targetEditorError,
    setTargetEditorError,
    setFormData,
    setIsEditModelDialogOpen,
    setLoadbalanceStrategyId,
    handleEditModelSubmit,
  };
}
