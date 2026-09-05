import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { ModelConfig } from "@/lib/types";
import {
  createEditModelFormData,
  DEFAULT_MODEL_FORM_DATA,
  setLoadbalanceStrategyIdOnForm,
  toModelUpdatePayload,
  type ModelFormData,
  type SubmitEventLike,
  validateModelFormData,
} from "../models/modelFormState";

interface UseModelDetailModelFormInput {
  model: ModelConfig | null;
  revision: number;
  setIsEditModelDialogOpenState: (open: boolean) => void;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
  refreshDiagnostics?: () => void | Promise<void>;
  refreshModels?: () => Promise<void>;
}

export function useModelDetailModelForm({
  model,
  revision,
  setIsEditModelDialogOpenState,
  setModel,
  refreshDiagnostics,
  refreshModels,
}: UseModelDetailModelFormInput) {
  const [formData, setFormData] = useState<ModelFormData>(DEFAULT_MODEL_FORM_DATA);
  const [targetEditorError, setTargetEditorError] = useState<string | null>(null);
  // 编辑对话框自己的错误位。与访问目标卡共用一个 state 时，保存失败的原因
  // 会同时出现在对话框里和它背后的卡片上。
  const [modelFormError, setModelFormError] = useState<string | null>(null);

  const applyUpdatedModel = useCallback(
    (updatedModel: ModelConfig) => {
      clearSharedReferenceData(undefined, revision);
      setModel(updatedModel);
    },
    [revision, setModel],
  );

  const setLoadbalanceStrategyId = useCallback((value: number | null) => {
    setFormData((current) => setLoadbalanceStrategyIdOnForm(current, value));
  }, []);

  const setIsEditModelDialogOpen = useCallback(
    (open: boolean) => {
      if (open) {
        setFormData(model ? createEditModelFormData(model) : DEFAULT_MODEL_FORM_DATA);
        setModelFormError(null);
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
      setModelFormError(null);
      const validationError = validateModelFormData(formData);

      // 对话框开着时只在框内报错，关闭之后才用 toast 报成功。
      if (validationError === "api_family_required") {
        setModelFormError(messages.modelDetailData.selectApiFamily);
        return;
      }

      if (validationError === "loadbalance_strategy_required") {
        setModelFormError(messages.modelDetailData.selectLoadbalanceStrategy);
        return;
      }

      if (validationError === "model_id_required") {
        setModelFormError(messages.modelsData.modelIdRequired);
        return;
      }

      try {
        const updatedResponse = await api.models.update(model.id, toModelUpdatePayload(formData));
        applyUpdatedModel(updatedResponse.model);
        await refreshModels?.();
        void refreshDiagnostics?.();
        toast.success(messages.modelDetailData.modelUpdated);
        setIsEditModelDialogOpen(false);
      } catch (error) {
        setModelFormError(
          error instanceof Error
            ? error.message
            : messages.modelDetailData.updateModelFailed,
        );
      }
    },
    [
      applyUpdatedModel,
      formData,
      model,
      refreshDiagnostics,
      refreshModels,
      setIsEditModelDialogOpen,
    ],
  );

  return {
    formData,
    targetEditorError,
    setTargetEditorError,
    modelFormError,
    setModelFormError,
    setFormData,
    setIsEditModelDialogOpen,
    setLoadbalanceStrategyId,
    handleEditModelSubmit,
  };
}
