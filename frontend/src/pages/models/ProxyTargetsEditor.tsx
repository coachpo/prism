import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Route, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyTarget } from "@/lib/types";
import {
  appendProxyTarget,
  moveProxyTarget,
  normalizeProxyTargets,
  removeProxyTarget,
} from "./modelFormState";

interface ProxyTargetsEditorProps {
  apiFamilyLabel: string;
  availableTargets: { modelId: string; label: string }[];
  proxyTargets: ProxyTarget[];
  onChange: (proxyTargets: ProxyTarget[]) => void;
}

export function ProxyTargetsEditor({
  apiFamilyLabel,
  availableTargets,
  proxyTargets,
  onChange,
}: ProxyTargetsEditorProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const [pendingTargetId, setPendingTargetId] = useState("");
  const normalizedTargets = useMemo(() => normalizeProxyTargets(proxyTargets), [proxyTargets]);

  const remainingTargets = useMemo(() => {
    const selectedTargetIds = new Set(normalizedTargets.map((target) => target.target_model_id));
    return availableTargets.filter((target) => !selectedTargetIds.has(target.modelId));
  }, [availableTargets, normalizedTargets]);

  const effectivePendingTargetId = remainingTargets.some((target) => target.modelId === pendingTargetId)
    ? pendingTargetId
    : "";

  const resolveTargetLabel = (targetModelId: string) => {
    return availableTargets.find((target) => target.modelId === targetModelId)?.label ?? targetModelId;
  };

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/15 p-4">
      <div className="flex items-start gap-2">
        <Route className="mt-0.5 h-4 w-4 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{detailCopy.proxyTargets}</p>
          <p className="text-sm text-muted-foreground">{copy.proxyTargetsDescriptionPrimary}</p>
          <p className="text-sm text-muted-foreground">{copy.proxyTargetsDescriptionSecondary}</p>
        </div>
      </div>

      {normalizedTargets.length === 0 ? (
        <div className="rounded-md border border-dashed border-border bg-background px-3 py-2 text-sm text-muted-foreground">
          {copy.noProxyTargetsSelected}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {normalizedTargets.map((target, index) => (
            <div key={target.target_model_id} className="flex items-center justify-between gap-3 rounded-md border bg-background px-3 py-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{resolveTargetLabel(target.target_model_id)}</p>
                <p className="text-xs text-muted-foreground">{copy.priority(formatNumber(index + 1))}</p>
              </div>
              <div className="flex items-center gap-1">
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={copy.targetMoveUp(target.target_model_id)}
                  disabled={index === 0}
                  onClick={() => onChange(moveProxyTarget(normalizedTargets, index, index - 1))}
                >
                  <ArrowUp />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={copy.targetMoveDown(target.target_model_id)}
                  disabled={index === normalizedTargets.length - 1}
                  onClick={() => onChange(moveProxyTarget(normalizedTargets, index, index + 1))}
                >
                  <ArrowDown />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={copy.targetRemove(target.target_model_id)}
                  onClick={() => onChange(removeProxyTarget(normalizedTargets, target.target_model_id))}
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {availableTargets.length === 0 ? (
        <p className="text-sm text-muted-foreground">{copy.noNativeModelsForFamily(apiFamilyLabel)}</p>
      ) : null}

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <p className="text-xs text-muted-foreground">
          {remainingTargets.length === 0
            ? copy.allNativeModelsIncluded
            : copy.remainingNativeTargets(formatNumber(remainingTargets.length))}
        </p>
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
          <Select
            value={pendingTargetId}
            onValueChange={setPendingTargetId}
            disabled={remainingTargets.length === 0}
          >
            <SelectTrigger id="proxy-target-select" className="w-full min-w-0 sm:min-w-72">
              <SelectValue placeholder={detailCopy.modelIdLabel} />
            </SelectTrigger>
            <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
              {remainingTargets.map((target) => (
                <SelectItem key={target.modelId} value={target.modelId}>
                  <span className="block whitespace-normal break-words pr-4 leading-5">{target.label}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="outline"
            disabled={!effectivePendingTargetId}
            onClick={() => {
              onChange(appendProxyTarget(normalizedTargets, effectivePendingTargetId));
              setPendingTargetId("");
            }}
          >
            {copy.addTarget}
          </Button>
        </div>
      </div>
    </div>
  );
}
