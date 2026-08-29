import { useCallback, useEffect, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedLoadbalanceStrategies,
  getSharedModels,
  setSharedLoadbalanceStrategies,
  setSharedModels,
} from "@/lib/referenceData";
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import type { LoadbalanceStrategy } from "@/lib/types";
import { toast } from "sonner";

function sortStrategies(strategies: LoadbalanceStrategy[]) {
  return [...strategies].sort((left, right) => {
    const updatedAtDelta =
      new Date(right.updated_at).getTime() -
      new Date(left.updated_at).getTime();
    return updatedAtDelta !== 0 ? updatedAtDelta : right.id - left.id;
  });
}

export function useModelsCollection(revision: number) {
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<
    LoadbalanceStrategy[]
  >([]);
  const [models, setModels] = useState<ManagedModelConfigListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadAttempt, setLoadAttempt] = useState(0);

  const applyBootstrapData = useCallback(
    (data: {
      loadbalanceStrategiesData: LoadbalanceStrategy[];
      modelsData: ManagedModelConfigListItem[];
    }) => {
      setLoadbalanceStrategies(data.loadbalanceStrategiesData);
      setModels(data.modelsData);
    },
    [],
  );

  const fetchData = useCallback(async (currentRevision: number) => {
    return Promise.all([
      getSharedLoadbalanceStrategies(currentRevision),
      getSharedModels(currentRevision),
    ]).then(([loadbalanceStrategiesData, modelsData]) => ({
      loadbalanceStrategiesData,
      modelsData: modelsData as ManagedModelConfigListItem[],
    }));
  }, []);

  useEffect(() => {
    let cancelled = false;

    setLoading(true);
    setLoadError(null);
    void (async () => {
      try {
        const data = await fetchData(revision);
        if (cancelled) return;
        applyBootstrapData(data);
        setLoadError(null);
      } catch (error) {
        if (!cancelled) {
          const messages = getStaticMessages();
          const message =
            error instanceof Error
              ? error.message
              : messages.modelsData.fetchFailed;
          setLoadError(message);
          toast.error(messages.modelsData.fetchFailed);
          console.error(error);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [applyBootstrapData, fetchData, loadAttempt, revision]);

  const retryLoad = useCallback(() => {
    setLoadAttempt((current) => current + 1);
  }, []);

  const refreshModels = useCallback(async () => {
    const next = (await getSharedModels(
      revision,
      true,
    )) as ManagedModelConfigListItem[];
    setModels(next);
    setSharedModels(revision, next);
    return next;
  }, [revision]);

  const commitModels = useCallback(
    (
      updater: (
        current: ManagedModelConfigListItem[],
      ) => ManagedModelConfigListItem[],
    ) => {
      setModels((current) => {
        const next = updater(current);
        setSharedModels(revision, next);
        return next;
      });
    },
    [revision],
  );

  const readSortedLoadbalanceStrategies = useCallback(
    async (forceRefresh = false) => {
      const strategies = forceRefresh
        ? await getSharedLoadbalanceStrategies(revision, true)
        : await getSharedLoadbalanceStrategies(revision);
      return sortStrategies(strategies);
    },
    [revision],
  );

  const publishLoadbalanceStrategies = useCallback(
    (strategies: LoadbalanceStrategy[]) => {
      setSharedLoadbalanceStrategies(revision, strategies);
    },
    [revision],
  );

  const replaceLoadbalanceStrategies = useCallback(
    (strategies: LoadbalanceStrategy[]) => {
      setLoadbalanceStrategies(sortStrategies(strategies));
    },
    [],
  );

  const refreshStrategiesAfterDialogClose = useCallback(() => {
    void readSortedLoadbalanceStrategies().then(replaceLoadbalanceStrategies);
  }, [readSortedLoadbalanceStrategies, replaceLoadbalanceStrategies]);

  return {
    commitModels,
    loadError,
    loadbalanceStrategies,
    loading,
    models,
    publishLoadbalanceStrategies,
    readSortedLoadbalanceStrategies,
    refreshStrategiesAfterDialogClose,
    replaceLoadbalanceStrategies,
    refreshModels,
    retryLoad,
  };
}
