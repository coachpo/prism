import { useCallback, useState } from "react";

import { api } from "@/lib/api";
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import { getStaticMessages } from "@/i18n/staticMessages";
import { toast } from "sonner";
import { toModelListItem } from "./modelListProjection";

interface UseModelEnablementMutationsInput {
  commitModels: (
    updater: (
      current: ManagedModelConfigListItem[],
    ) => ManagedModelConfigListItem[],
  ) => void;
}

export function useModelEnablementMutations({
  commitModels,
}: UseModelEnablementMutationsInput) {
  const [togglingModelIds, setTogglingModelIds] = useState<Set<number>>(
    new Set(),
  );

  const setModelEnabled = useCallback(
    async (model: ManagedModelConfigListItem, nextEnabled: boolean) => {
      const messages = getStaticMessages();
      setTogglingModelIds((current) => new Set(current).add(model.id));
      try {
        const updated = await api.models.update(model.id, {
          is_enabled: nextEnabled,
        });
        commitModels((current) =>
          current.map((item) =>
            item.id === model.id
              ? toModelListItem(updated.model, item)
              : item,
          ),
        );
        return true;
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : messages.modelsPage.toggleFailed,
        );
        return false;
      } finally {
        setTogglingModelIds((current) => {
          const next = new Set(current);
          next.delete(model.id);
          return next;
        });
      }
    },
    [commitModels],
  );

  const setModelsEnabled = useCallback(
    async (targets: ManagedModelConfigListItem[], nextEnabled: boolean) => {
      const messages = getStaticMessages();
      const results = await Promise.all(
        targets.map((model) => setModelEnabled(model, nextEnabled)),
      );
      const succeeded = results.filter(Boolean).length;
      const failed = results.length - succeeded;
      toast.success(
        messages.modelsPage.bulkDone(String(succeeded), String(failed)),
      );
    },
    [setModelEnabled],
  );

  return { setModelEnabled, setModelsEnabled, togglingModelIds };
}
