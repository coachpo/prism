import { useCallback, useEffect, useRef } from "react";
import type { Dispatch, SetStateAction } from "react";
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
  setModel: Dispatch<SetStateAction<ModelConfig | null>>;
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  setAllConnections: Dispatch<SetStateAction<Connection[]>>;
  setGlobalEndpoints: Dispatch<SetStateAction<Endpoint[]>>;
  setLoadbalanceStrategies: Dispatch<SetStateAction<LoadbalanceStrategy[]>>;
  setAllModels: Dispatch<SetStateAction<ModelConfigListItem[]>>;
  setPricingTemplates: Dispatch<SetStateAction<PricingTemplate[]>>;
  setLoading: Dispatch<SetStateAction<boolean>>;
  /** 模型本体读取失败的原因；页面留在原地展示错误面。 */
  setLoadError: Dispatch<SetStateAction<string | null>>;
  /** 这个模型配置不存在（404）。与「读取失败」是两件事，页面分开渲染。 */
  setNotFound: Dispatch<SetStateAction<boolean>>;
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
  setModel,
  setConnections,
  setAllConnections,
  setGlobalEndpoints,
  setLoadbalanceStrategies,
  setAllModels,
  setPricingTemplates,
  setLoading,
  setLoadError,
  setNotFound,
  setDegradedParts,
}: UseModelDetailBootstrapInput) {
  const modelRequestIdRef = useRef(0);
  const fetchModel = useCallback(async () => {
    if (!id) return;

    const modelConfigId = Number.parseInt(id, 10);
    if (!Number.isFinite(modelConfigId)) {
      // 路由参数不是合法标识符：保留 URL，由页面就地说明是链接抄错了，
      // 而不是零提示地把人弹回列表、让他以为是数据少了一行。
      setLoading(false);
      return;
    }

    const requestId = ++modelRequestIdRef.current;
    setLoadError(null);
    setNotFound(false);

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
      // 「这个模型配置不存在」和「这次没读到」必须长得不一样，也都保留 URL
      // 与页面上下文：一 toast 就跳走会让操作者分不清链接失效还是网关抖动。
      if (error instanceof ApiError && error.status === 404) {
        setNotFound(true);
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
    setNotFound,
    setDegradedParts,
    ]);

  useEffect(() => {
    void fetchModel();
  }, [fetchModel]);

  return { fetchModel };
}
