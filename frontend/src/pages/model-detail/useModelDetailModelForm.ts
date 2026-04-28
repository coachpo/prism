import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { ModelConfig, ModelConfigListItem } from "@/lib/types";
import {
  createEditModelFormData,
  DEFAULT_MODEL_FORM_DATA,
  getNativeModelsForApiFamily,
  setLoadbalanceStrategyIdOnForm,
  toModelUpdatePayload,
  type ModelFormData,
  type SubmitEventLike,
  validateModelFormData,
} from "../models/modelFormState";
import {
  buildProxyTargetOptions,
  patchModelListItemFromDetail,
} from "./useModelDetailDataSupport";

interface UseModelDetailModelFormInput {
  model: ModelConfig | null;
  allModels: ModelConfigListItem[];
  revision: number;
  setIsEditModelDialogOpenState: (open: boolean) => void;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
}

export function useModelDetailModelForm({
  model,
  allModels,
  revision,
  setIsEditModelDialogOpenState,
  setAllModels,
  setModel,
}: UseModelDetailModelFormInput) {
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);

  const proxyTargetOptions = useMemo(() => buildProxyTargetOptions(model, allModels), [allModels, model]);
  const nativeModelsForApiFamily = useMemo(
    () => getNativeModelsForApiFamily(allModels, formData.api_family, model?.model_id),
    [allModels, formData.api_family, model?.model_id],
  );

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
      const validationError = validateModelFormData(
        formData,
        nativeModelsForApiFamily.map((candidate) => candidate.model_id),
      );

      if (validationError === "api_family_required") {
        toast.error(messages.modelDetailData.selectApiFamily);
        return;
      }

      if (validationError === "loadbalance_strategy_required") {
        toast.error(messages.modelDetailData.selectLoadbalanceStrategy);
        return;
      }

      if (validationError === "proxy_target_required") {
        toast.error(messages.modelsData.proxyTargetRequired);
        return;
      }

      try {
        const updatedModel = await api.models.update(model.id, toModelUpdatePayload(formData));
        applyUpdatedModel(updatedModel);
        toast.success(messages.modelDetailData.modelUpdated);
        setIsEditModelDialogOpen(false);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : messages.modelDetailData.updateModelFailed);
      }
    },
    [applyUpdatedModel, formData, model, nativeModelsForApiFamily, setIsEditModelDialogOpen],
  );

  return {
    formData,
    nativeModelsForApiFamily,
    proxyTargetOptions,
    setFormData,
    setIsEditModelDialogOpen,
    setLoadbalanceStrategyId,
    handleEditModelSubmit,
  };
}
