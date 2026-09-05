import { useCallback, useEffect, useRef } from "react";
import type { Dispatch, SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/request";
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
} from "@/lib/types";
import { getOwnedModelConnections } from "./modelAccessTargetProjection";

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
  /** 模型本体读取失败的原因；非 404 时页面留在原地展示错误面。 */
  setLoadError: Dispatch<SetStateAction<string | null>>;
  setDegradedParts: Dispatch<SetStateAction<ModelDetailDegradedParts>>;
}

/**
 * 次要数据读取失败时页面主体仍要渲染，但绝不能把失败装扮成空集合：
 * 失败原因随值一起带出去，由消费方决定在哪里说出来。
 */
function resolveOptionalBootstrapValue<T>(
  result: PromiseSettledResult<T>,
  fallback: T,
  label: string,
): { value: T; error: string | null } {
  if (result.status === "fulfilled") {
    return { value: result.value, error: null };
  }

  console.error(`Failed to load ${label}`, result.reason);
  return {
    value: fallback,
    error:
      result.reason instanceof Error
        ? result.reason.message
        : getStaticMessages().common.requestFailed,
  };
}

/** 哪些次要数据源这次没读到；null 表示读到了。 */
export interface ModelDetailDegradedParts {
  endpoints: string | null;
  loadbalanceStrategies: string | null;
  models: string | null;
  pricingTemplates: string | null;
}

export const NO_DEGRADED_PARTS: ModelDetailDegradedParts = {
  endpoints: null,
  loadbalanceStrategies: null,
  models: null,
  pricingTemplates: null,
};

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
  setLoadError,
  setDegradedParts,
}: UseModelDetailBootstrapInput) {
  const modelRequestIdRef = useRef(0);
  const fetchModel = useCallback(async () => {
    if (!id) return;

    const modelConfigId = Number.parseInt(id, 10);
    if (!Number.isFinite(modelConfigId)) {
      navigate("/route/models");
      return;
    }

    const requestId = ++modelRequestIdRef.current;
    setLoadError(null);

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

      const endpoints = resolveOptionalBootstrapValue(
        endpointsResult,
        [],
        "model-detail endpoints",
      );
      const loadbalanceStrategies = resolveOptionalBootstrapValue(
        loadbalanceStrategiesResult,
        [],
        "model-detail loadbalance strategies",
      );
      const models = resolveOptionalBootstrapValue(
        modelsResult,
        [],
        "model-detail models",
      );
      const pricingTemplates = resolveOptionalBootstrapValue(
        pricingTemplatesResult,
        [],
        "model-detail pricing templates",
      );

      setModel(data);
      setConnections(getOwnedModelConnections(data, modelConfigId));
      setAllConnections(connectionsList.filter((connection) => connection.model_config_id == null || connection.model_config_id === modelConfigId));
      setGlobalEndpoints(endpoints.value);
      setLoadbalanceStrategies(loadbalanceStrategies.value);
      setAllModels(models.value);
      setPricingTemplates(pricingTemplates.value);
      setDegradedParts({
        endpoints: endpoints.error,
        loadbalanceStrategies: loadbalanceStrategies.error,
        models: models.error,
        pricingTemplates: pricingTemplates.error,
      });

    } catch (error) {
      if (requestId !== modelRequestIdRef.current) {
        return;
      }
      console.error(error);
      // 只有「这个模型配置不存在」才该把人送回列表：其它读取失败保留 URL
      // 与页面上下文，让操作者原地重试，而不是丢掉他找到这一行的过程。
      if (error instanceof ApiError && error.status === 404) {
        toast.error(getStaticMessages().modelDetailData.modelConfigNotFound);
        navigate("/route/models");
        return;
      }
      setLoadError(
        error instanceof Error
          ? error.message
          : getStaticMessages().common.requestFailed,
      );
    } finally {
      if (requestId === modelRequestIdRef.current) {
        setLoading(false);
      }
    }
  }, [
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
    setPricingTemplates,
    setLoadError,
    setDegradedParts,
    ]);

  useEffect(() => {
    void fetchModel();
  }, [fetchModel]);

  return { fetchModel };
}
