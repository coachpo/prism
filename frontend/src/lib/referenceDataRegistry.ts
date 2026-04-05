import { api } from "@/lib/api";
import type {
  ConnectionDropdownItem,
  Endpoint,
  LoadbalanceStrategy,
  ModelConfigListItem,
  PricingTemplate,
  Vendor,
} from "@/lib/types";

export interface ReferenceDataMap {
  connections: ConnectionDropdownItem[];
  endpoints: Endpoint[];
  loadbalanceStrategies: LoadbalanceStrategy[];
  models: ModelConfigListItem[];
  pricingTemplates: PricingTemplate[];
  vendors: Vendor[];
}

export type ReferenceDataKind = keyof ReferenceDataMap;

type ReferenceDataRegistryEntry<T> = {
  load: () => Promise<T>;
};

export type ReferenceDataRegistry = {
  [K in ReferenceDataKind]: ReferenceDataRegistryEntry<ReferenceDataMap[K]>;
};

type RegistryKey<TRegistry> = Extract<keyof TRegistry, string>;
type RegistryData<
  TRegistry extends Record<string, ReferenceDataRegistryEntry<unknown>>,
  K extends RegistryKey<TRegistry>,
> = Awaited<ReturnType<TRegistry[K]["load"]>>;

export const referenceDataRegistry = {
  connections: {
    load: async () => {
      const response = await api.endpoints.connections();
      return response.items;
    },
  },
  endpoints: {
    load: () => api.endpoints.list(),
  },
  loadbalanceStrategies: {
    load: () => api.loadbalanceStrategies.list(),
  },
  models: {
    load: () => api.models.list(),
  },
  pricingTemplates: {
    load: () => api.pricingTemplates.list(),
  },
  vendors: {
    load: () => api.vendors.list(),
  },
} satisfies ReferenceDataRegistry;

function buildCacheKey(kind: string, revision: number) {
  return `${kind}:${revision}`;
}

function pruneCacheEntries(
  kind: string,
  revision: number,
  dataCache: Map<string, unknown>,
  requestCache: Map<string, Promise<unknown>>,
) {
  const prefix = `${kind}:`;
  const activeKey = buildCacheKey(kind, revision);

  for (const key of dataCache.keys()) {
    if (key.startsWith(prefix) && key !== activeKey) {
      dataCache.delete(key);
    }
  }

  for (const key of requestCache.keys()) {
    if (key.startsWith(prefix) && key !== activeKey) {
      requestCache.delete(key);
    }
  }
}

function clearCacheEntries(
  dataCache: Map<string, unknown>,
  requestCache: Map<string, Promise<unknown>>,
  kind?: string,
  revision?: number,
) {
  if (kind === undefined && revision === undefined) {
    dataCache.clear();
    requestCache.clear();
    return;
  }

  for (const key of dataCache.keys()) {
    const [entryKind, entryRevision] = key.split(":");
    const kindMatches = kind === undefined || entryKind === kind;
    const revisionMatches = revision === undefined || entryRevision === String(revision);

    if (kindMatches && revisionMatches) {
      dataCache.delete(key);
    }
  }

  for (const key of requestCache.keys()) {
    const [entryKind, entryRevision] = key.split(":");
    const kindMatches = kind === undefined || entryKind === kind;
    const revisionMatches = revision === undefined || entryRevision === String(revision);

    if (kindMatches && revisionMatches) {
      requestCache.delete(key);
    }
  }
}

export function createReferenceDataStore<
  TRegistry extends Record<string, ReferenceDataRegistryEntry<unknown>>,
>(registry: TRegistry) {
  const dataCache = new Map<string, unknown>();
  const requestCache = new Map<string, Promise<unknown>>();

  return {
    async get<K extends RegistryKey<TRegistry>>(
      kind: K,
      revision: number,
      forceRefresh = false,
    ): Promise<RegistryData<TRegistry, K>> {
      const key = buildCacheKey(kind, revision);

      if (forceRefresh) {
        dataCache.delete(key);
        requestCache.delete(key);
      }

      if (dataCache.has(key)) {
        return dataCache.get(key) as RegistryData<TRegistry, K>;
      }

      const inFlight = requestCache.get(key);
      if (inFlight) {
        return inFlight as Promise<RegistryData<TRegistry, K>>;
      }

      const request = registry[kind]
        .load()
        .then((data) => {
          pruneCacheEntries(kind, revision, dataCache, requestCache);
          dataCache.set(key, data);
          return data as RegistryData<TRegistry, K>;
        })
        .finally(() => {
          if (requestCache.get(key) === request) {
            requestCache.delete(key);
          }
        });

      requestCache.set(key, request);
      return request as Promise<RegistryData<TRegistry, K>>;
    },

    set<K extends RegistryKey<TRegistry>>(
      kind: K,
      revision: number,
      data: RegistryData<TRegistry, K>,
    ) {
      pruneCacheEntries(kind, revision, dataCache, requestCache);
      dataCache.set(buildCacheKey(kind, revision), data);
    },

    clear(kind?: RegistryKey<TRegistry>, revision?: number) {
      clearCacheEntries(dataCache, requestCache, kind, revision);
    },
  };
}
