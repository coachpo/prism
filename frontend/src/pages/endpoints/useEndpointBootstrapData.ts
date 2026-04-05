import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getSharedEndpoints, setSharedEndpoints } from "@/lib/referenceData";
import type { Endpoint, ModelConfigListItem } from "@/lib/types";

export type EndpointModelsMap = Record<number, ModelConfigListItem[]>;

interface EndpointBootstrapData {
  endpointModels: EndpointModelsMap;
  endpoints: Endpoint[];
}

async function fetchEndpointBootstrapData(
  revision: number,
  reuseInFlight = false,
): Promise<EndpointBootstrapData> {
  const endpoints = await getSharedEndpoints(revision, !reuseInFlight);

  if (endpoints.length === 0) {
    return { endpointModels: {}, endpoints };
  }

  try {
    const response = await api.models.byEndpoints({
      endpoint_ids: endpoints.map((endpoint) => endpoint.id),
    });
    const endpointModels = Object.fromEntries(
      response.items.map((item) => [item.endpoint_id, item.models]),
    ) as EndpointModelsMap;

    return { endpointModels, endpoints };
  } catch (error) {
    console.error("Failed to load endpoint models", error);
    return { endpointModels: {}, endpoints };
  }
}

export function useEndpointBootstrapData(revision: number) {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [endpointModels, setEndpointModels] = useState<EndpointModelsMap>({});
  const [isLoading, setIsLoading] = useState(true);

  const applyBootstrapData = useCallback((data: EndpointBootstrapData) => {
    setEndpoints(data.endpoints);
    setEndpointModels(data.endpointModels);
  }, []);

  const commitEndpoints = useCallback(
    (
      endpointUpdater: (current: Endpoint[]) => Endpoint[],
      endpointModelsUpdater?: (current: EndpointModelsMap) => EndpointModelsMap,
    ) => {
      setEndpoints((current) => {
        const next = endpointUpdater(current);
        setSharedEndpoints(revision, next);
        return next;
      });

      if (endpointModelsUpdater) {
        setEndpointModels(endpointModelsUpdater);
      }
    },
    [revision],
  );

  useEffect(() => {
    setIsLoading(true);
    let cancelled = false;

    void (async () => {
      try {
        const data = await fetchEndpointBootstrapData(revision, true);
        if (cancelled) {
          return;
        }

        applyBootstrapData(data);
      } catch {
        if (!cancelled) {
          toast.error(getStaticMessages().endpointsData.loadFailed);
        }
      } finally {
        if (!cancelled) {
          setIsLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [applyBootstrapData, revision]);

  return {
    commitEndpoints,
    endpointModels,
    endpoints,
    isLoading,
    setEndpoints,
  };
}
