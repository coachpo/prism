import { useCallback, useState } from "react";

import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { LoadbalanceStrategy } from "@/lib/types";
import { toast } from "sonner";
import {
  setLoadbalanceStrategyIdOnForm,
  type ModelFormData,
} from "./modelFormState";
import type { ModelDialogSession } from "./useModelDialogMutations";

interface UseLoadbalanceStrategyDefaultsInput {
  dialogSessionRef: React.MutableRefObject<ModelDialogSession>;
  publishLoadbalanceStrategies: (strategies: LoadbalanceStrategy[]) => void;
  readSortedLoadbalanceStrategies: (
    forceRefresh?: boolean,
  ) => Promise<LoadbalanceStrategy[]>;
  replaceLoadbalanceStrategies: (strategies: LoadbalanceStrategy[]) => void;
  setFormData: React.Dispatch<React.SetStateAction<ModelFormData>>;
}

export function useLoadbalanceStrategyDefaults({
  dialogSessionRef,
  publishLoadbalanceStrategies,
  readSortedLoadbalanceStrategies,
  replaceLoadbalanceStrategies,
  setFormData,
}: UseLoadbalanceStrategyDefaultsInput) {
  const [loadbalanceStrategyDefaultsCreating, setCreating] = useState(false);

  const handleCreateLoadbalanceStrategyDefaults = useCallback(async () => {
    const messages = getStaticMessages();
    setCreating(true);
    try {
      const response = await api.loadbalanceStrategies.createDefaults();
      const next = await readSortedLoadbalanceStrategies(true);
      publishLoadbalanceStrategies(next);
      const currentDialog = dialogSessionRef.current;
      if (currentDialog.mode === "create") {
        replaceLoadbalanceStrategies(next);
        setFormData((current) =>
          setLoadbalanceStrategyIdOnForm(current, next[0]?.id ?? null),
        );
      } else if (currentDialog.mode === "closed") {
        replaceLoadbalanceStrategies(next);
      }
      toast.success(
        response.created.length > 0
          ? messages.loadbalanceStrategiesData.defaultsCreated
          : messages.loadbalanceStrategiesData.defaultsAlreadyExisted,
      );
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.loadbalanceStrategiesData.saveFailed,
      );
    } finally {
      setCreating(false);
    }
  }, [
    dialogSessionRef,
    publishLoadbalanceStrategies,
    readSortedLoadbalanceStrategies,
    replaceLoadbalanceStrategies,
    setFormData,
  ]);

  return {
    handleCreateLoadbalanceStrategyDefaults,
    loadbalanceStrategyDefaultsCreating,
  };
}
