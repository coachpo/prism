import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getSharedModels } from "@/lib/referenceData";
import type { CostingSettingsUpdate, ModelConfigListItem } from "@/lib/types";
import { toast } from "sonner";
import { DEFAULT_COSTING_FORM, normalizeCostingForm } from "../settingsPageHelpers";

function getMessages() {
  return getStaticMessages();
}

export function useCostingSettingsBootstrap(revision: number) {
  const [costingUnavailable, setCostingUnavailable] = useState(false);
  const [costingLoading, setCostingLoading] = useState(false);
  const [savedCostingForm, setSavedCostingForm] = useState<CostingSettingsUpdate | null>(null);
  const [costingForm, setCostingForm] = useState<CostingSettingsUpdate>(DEFAULT_COSTING_FORM);
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const modelsRequestIdRef = useRef(0);
  const costingRequestIdRef = useRef(0);

  const fetchModels = useCallback(async () => {
    const requestId = ++modelsRequestIdRef.current;
    try {
      const data = await getSharedModels(revision);
      if (requestId !== modelsRequestIdRef.current) {
        return;
      }
      setModels(data);
    } catch {
      if (requestId !== modelsRequestIdRef.current) {
        return;
      }
      toast.error(getMessages().settingsCostingData.loadModelsForFxFailed);
    }
  }, [revision]);

  const fetchCostingSettings = useCallback(async () => {
    const requestId = ++costingRequestIdRef.current;
    setCostingLoading(true);
    try {
      const data = await api.settings.costing.get();
      if (requestId !== costingRequestIdRef.current) {
        return;
      }
      const normalized = normalizeCostingForm(data);
      setCostingForm(normalized);
      setSavedCostingForm(normalized);
      setCostingUnavailable(false);
    } catch (error) {
      if (requestId !== costingRequestIdRef.current) {
        return;
      }
      toast.error(error instanceof Error ? error.message : getMessages().settingsCostingData.loadCostingFailed);
    } finally {
      if (requestId === costingRequestIdRef.current) {
        setCostingLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    void fetchCostingSettings();
    void fetchModels();
  }, [fetchCostingSettings, fetchModels]);

  return {
    costingForm,
    costingLoading,
    costingUnavailable,
    models,
    savedCostingForm,
    setCostingForm,
    setCostingUnavailable,
    setSavedCostingForm,
  };
}
