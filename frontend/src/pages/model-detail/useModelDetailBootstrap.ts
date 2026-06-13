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
import { getOwnedModelConnections } from "./useModelDetailDataSupport";

interface UseModelDetailBootstrapInput {
  id: string | undefined;
  revision: number;
  navigate: (to: string) => void;
  setModel: Dispatch<SetStateAction<ModelConfig | null>>;
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  setAllConnections: Dispatch<SetStateAction<Connection[]>>;
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

function resolveOptionalBootstrapValue<T>(
  result: PromiseSettledResult<T>,
  fallback: T,
  label: string,
): T {
  if (result.status === "fulfilled") {
    return result.value;
  }

  console.error(`Failed to load ${label}`, result.reason);
  return fallback;
}

export function useModelDetailBootstrap({
  id,
  revision,
  navigate,
  setModel,
  setConnections,
  setAllConnections,
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

    const modelConfigId = Number.parseInt(id, 10);
    if (!Number.isFinite(modelConfigId)) {
      navigate("/models");
      return;
    }

    const requestId = ++modelRequestIdRef.current;
    spendingRequestIdRef.current += 1;
    setSpending(null);

    try {
      const [data, connectionsList] = await Promise.all([
        api.models.get(modelConfigId),
        api.models.connections.list(modelConfigId),
      ]);
      const [
        endpointsResult,
        loadbalanceStrategiesResult,
        modelsResult,
        pricingTemplatesResult,
      ] = await Promise.allSettled([
        getSharedEndpoints(revision),
        getSharedLoadbalanceStrategies(revision),
        getSharedModels(revision),
        getSharedPricingTemplates(revision),
      ]);

      if (requestId !== modelRequestIdRef.current) {
        return;
      }

      setModel(data);
      setConnections(getOwnedModelConnections(data, modelConfigId));
      setAllConnections(connectionsList.filter((connection) => connection.model_config_id == null || connection.model_config_id === modelConfigId));
      setGlobalEndpoints(resolveOptionalBootstrapValue(endpointsResult, [], "model-detail endpoints"));
      setLoadbalanceStrategies(resolveOptionalBootstrapValue(loadbalanceStrategiesResult, [], "model-detail loadbalance strategies"));
      setAllModels(resolveOptionalBootstrapValue(modelsResult, [], "model-detail models"));
      setPricingTemplates(resolveOptionalBootstrapValue(pricingTemplatesResult, [], "model-detail pricing templates"));

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
    setAllConnections,
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
