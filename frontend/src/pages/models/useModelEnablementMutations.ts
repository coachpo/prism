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

  /** 不带反馈的内部写入；成功提示与撤销由外层决定，避免批量时刷屏。 */
  const writeModelEnabled = useCallback(
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

  /**
   * 行内开关即点即写，不加确认 —— 但成功必须有反馈，且必须能撤销：
   * 扫描「现在健康吗」时误触一次，就是一个入口模型立刻停止路由。
   */
  const setModelEnabled = useCallback(
    async (model: ManagedModelConfigListItem, nextEnabled: boolean) => {
      const messages = getStaticMessages();
      const succeeded = await writeModelEnabled(model, nextEnabled);
      if (!succeeded) return false;
      const name = model.display_name?.trim() || model.model_id;
      toast.success(
        nextEnabled
          ? messages.modelsPage.toggleEnabledDone(name)
          : messages.modelsPage.toggleDisabledDone(name),
        {
          duration: 8000,
          action: {
            label: messages.modelsPage.toggleUndo,
            onClick: () => {
              void writeModelEnabled(model, !nextEnabled).then((reverted) => {
                if (reverted) toast.success(messages.modelsPage.toggleUndone);
              });
            },
          },
        },
      );
      return true;
    },
    [writeModelEnabled],
  );

  const setModelsEnabled = useCallback(
    async (targets: ManagedModelConfigListItem[], nextEnabled: boolean) => {
      const messages = getStaticMessages();
      const results = await Promise.all(
        targets.map((model) => writeModelEnabled(model, nextEnabled)),
      );
      const succeeded = results.filter(Boolean).length;
      const failed = results.length - succeeded;
      toast.success(
        messages.modelsPage.bulkDone(String(succeeded), String(failed)),
      );
    },
    [writeModelEnabled],
  );

  return { setModelEnabled, setModelsEnabled, togglingModelIds };
}
