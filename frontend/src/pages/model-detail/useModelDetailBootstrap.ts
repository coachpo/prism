import { useCallback, useEffect, useRef } from "react";
import type { Dispatch, SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedEndpoints,
  getSharedLoadbalanceStrategies,
  getSharedModels,
  getSharedPricingTemplates,
} from "@/lib/referenceData";
import type {
  Connection,
  Endpoint,
  LoadbalanceStrategy,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
  SpendingSummary,
} from "@/lib/types";

interface UseModelDetailBootstrapInput {
  id: string | undefined;
  revision: number;
  navigate: (to: string) => void;
  setModel: Dispatch<SetStateAction<ModelConfig | null>>;
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  setGlobalEndpoints: Dispatch<SetStateAction<Endpoint[]>>;
  setLoadbalanceStrategies: Dispatch<SetStateAction<LoadbalanceStrategy[]>>;
  setAllModels: Dispatch<SetStateAction<ModelConfigListItem[]>>;
  setPricingTemplates: Dispatch<SetStateAction<PricingTemplate[]>>;
  setLoading: Dispatch<SetStateAction<boolean>>;
  setSpending: Dispatch<SetStateAction<SpendingSummary | null>>;
  setSpendingLoading: Dispatch<SetStateAction<boolean>>;
  setSpendingCurrencySymbol: Dispatch<SetStateAction<string>>;
  setSpendingCurrencyCode: Dispatch<SetStateAction<string>>;
}

export function useModelDetailBootstrap({
  id,
  revision,
  navigate,
  setModel,
  setConnections,
  setGlobalEndpoints,
  setLoadbalanceStrategies,
  setAllModels,
  setPricingTemplates,
  setLoading,
  setSpending,
  setSpendingLoading,
  setSpendingCurrencySymbol,
  setSpendingCurrencyCode,
}: UseModelDetailBootstrapInput) {
  const modelRequestIdRef = useRef(0);
  const spendingRequestIdRef = useRef(0);

  const fetchSpending = useCallback(
    async (modelId: string) => {
      const requestId = ++spendingRequestIdRef.current;
      setSpendingLoading(true);
      try {
        const data = await api.stats.spending({
          model_id: modelId,
          group_by: "endpoint",
          preset: "all",
        });
        if (requestId !== spendingRequestIdRef.current) {
          return;
        }
        setSpending(data.summary);
        setSpendingCurrencySymbol(data.report_currency_symbol);
        setSpendingCurrencyCode(data.report_currency_code);
      } catch (error) {
        if (requestId !== spendingRequestIdRef.current) {
          return;
        }
        console.error("Failed to fetch spending", error);
      } finally {
        if (requestId === spendingRequestIdRef.current) {
          setSpendingLoading(false);
        }
      }
    },
    [setSpending, setSpendingCurrencyCode, setSpendingCurrencySymbol, setSpendingLoading]
  );

  const fetchModel = useCallback(async () => {
    if (!id) return;

    const requestId = ++modelRequestIdRef.current;
    spendingRequestIdRef.current += 1;
    setSpending(null);

    try {
      const [data, endpointsList, loadbalanceStrategiesList, modelsList, pricingTemplatesList] = await Promise.all([
        api.models.get(Number.parseInt(id, 10)),
        getSharedEndpoints(revision),
        getSharedLoadbalanceStrategies(revision),
        getSharedModels(revision),
        getSharedPricingTemplates(revision),
      ]);

      if (requestId !== modelRequestIdRef.current) {
        return;
      }

      setModel(data);
      setConnections(data.connections || []);
      setGlobalEndpoints(endpointsList);
      setLoadbalanceStrategies(loadbalanceStrategiesList);
      setAllModels(modelsList);
      setPricingTemplates(pricingTemplatesList);

      void fetchSpending(data.model_id);
    } catch (error) {
      if (requestId !== modelRequestIdRef.current) {
        return;
      }
      toast.error(getStaticMessages().modelDetailData.fetchModelDetailsFailed);
      console.error(error);
      navigate("/models");
    } finally {
      if (requestId === modelRequestIdRef.current) {
        setLoading(false);
      }
    }
  }, [
    fetchSpending,
    id,
    navigate,
    revision,
    setAllModels,
    setConnections,
    setGlobalEndpoints,
    setLoadbalanceStrategies,
    setLoading,
    setModel,
    setSpending,
    setPricingTemplates,
  ]);

  useEffect(() => {
    void fetchModel();
  }, [fetchModel]);

  return { fetchModel };
}
