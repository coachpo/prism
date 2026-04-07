import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { ModelConfig, ModelConfigListItem, ModelConfigUpdate, ProxyTarget } from "@/lib/types";
import {
  buildProxyTargetOptions,
  patchModelListItemFromDetail,
} from "./useModelDetailDataSupport";
import { normalizeProxyTargets } from "../models/modelFormState";

interface UseModelDetailModelFormInput {
  editLoadbalanceStrategyId: string;
  model: ModelConfig | null;
  allModels: ModelConfigListItem[];
  isEditModelDialogOpen: boolean;
  revision: number;
  setEditLoadbalanceStrategyId: (value: string) => void;
  setIsEditModelDialogOpen: (open: boolean) => void;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
}

export function useModelDetailModelForm({
  editLoadbalanceStrategyId,
  model,
  allModels,
  isEditModelDialogOpen,
  revision,
  setEditLoadbalanceStrategyId,
  setIsEditModelDialogOpen,
  setAllModels,
  setModel,
}: UseModelDetailModelFormInput) {
  useEffect(() => {
    if (!isEditModelDialogOpen || !model) {
      return;
    }

    setEditLoadbalanceStrategyId(model.loadbalance_strategy_id ? String(model.loadbalance_strategy_id) : "");
  }, [
    isEditModelDialogOpen,
    model,
    setEditLoadbalanceStrategyId,
  ]);

  const proxyTargetOptions = useMemo(
    () => buildProxyTargetOptions(model, allModels),
    [allModels, model],
  );
  const [proxyTargetsSaving, setProxyTargetsSaving] = useState(false);

  const applyUpdatedModel = useCallback(
    (updatedModel: ModelConfig) => {
      clearSharedReferenceData(undefined, revision);
      setModel(updatedModel);
      setAllModels((currentModels) => patchModelListItemFromDetail(currentModels, updatedModel));
      setEditLoadbalanceStrategyId(updatedModel.loadbalance_strategy_id ? String(updatedModel.loadbalance_strategy_id) : "");
    },
    [revision, setAllModels, setEditLoadbalanceStrategyId, setModel],
  );

  const handleEditModelSubmit = useCallback(
    async (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!model) {
        return;
      }

      if (model.model_type === "native" && !editLoadbalanceStrategyId) {
        toast.error(getStaticMessages().modelDetailData.selectLoadbalanceStrategy);
        return;
      }

      const formData = new FormData(event.currentTarget);
      const rawVendorId = String(formData.get("vendor_id") ?? "").trim();
      const vendorId = rawVendorId ? Number.parseInt(rawVendorId, 10) : null;
      const apiFamily = String(formData.get("api_family") ?? "").trim();

      if (!apiFamily) {
        toast.error(getStaticMessages().modelDetailData.selectApiFamily);
        return;
      }

      const updateData: ModelConfigUpdate = {
        vendor_id: vendorId,
        api_family: apiFamily as ModelConfigUpdate["api_family"],
        display_name: (formData.get("display_name") as string) || null,
        model_id: formData.get("model_id") as string,
        loadbalance_strategy_id:
          model.model_type === "native"
            ? Number.parseInt(editLoadbalanceStrategyId, 10) || null
            : null,
      };

      try {
        const updatedModel = await api.models.update(model.id, updateData);
        applyUpdatedModel(updatedModel);
        toast.success(getStaticMessages().modelDetailData.modelUpdated);
        setIsEditModelDialogOpen(false);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.updateModelFailed);
      }
    },
    [
      applyUpdatedModel,
      editLoadbalanceStrategyId,
      model,
      setIsEditModelDialogOpen,
    ],
  );

  const handleSaveProxyTargets = useCallback(
    async (proxyTargets: ProxyTarget[]) => {
      if (!model || model.model_type !== "proxy") {
        return;
      }

      if (proxyTargetsSaving) {
        return;
      }

      setProxyTargetsSaving(true);
      try {
        const updatedModel = await api.models.update(model.id, {
          proxy_targets: normalizeProxyTargets(proxyTargets),
        });
        applyUpdatedModel(updatedModel);
        toast.success(getStaticMessages().modelDetailData.proxyTargetsUpdated);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.updateProxyTargetsFailed);
      } finally {
        setProxyTargetsSaving(false);
      }
    },
    [applyUpdatedModel, model, proxyTargetsSaving],
  );

  return {
    proxyTargetOptions,
    proxyTargetsSaving,
    handleEditModelSubmit,
    handleSaveProxyTargets,
  };
}
