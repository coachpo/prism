import { useEffect, useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Route, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyTarget } from "@/lib/types";
import { appendProxyTarget, moveProxyTarget, normalizeProxyTargets, removeProxyTarget } from "../models/modelFormState";

interface ProxyTargetsCardProps {
  availableTargets: { modelId: string; label: string }[];
  proxyTargets: ProxyTarget[];
  saving: boolean;
  onSave: (proxyTargets: ProxyTarget[]) => void | Promise<void>;
}

export function ProxyTargetsCard({
  availableTargets,
  proxyTargets,
  saving,
  onSave,
}: ProxyTargetsCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const [draftTargets, setDraftTargets] = useState<ProxyTarget[]>(() => normalizeProxyTargets(proxyTargets));
  const [pendingTargetId, setPendingTargetId] = useState("");

  useEffect(() => {
    setDraftTargets(normalizeProxyTargets(proxyTargets));
  }, [proxyTargets]);

  const remainingTargets = useMemo(() => {
    const selectedTargetIds = new Set(draftTargets.map((target) => target.target_model_id));
    return availableTargets.filter((target) => !selectedTargetIds.has(target.modelId));
  }, [availableTargets, draftTargets]);

  const resolveTargetLabel = (targetModelId: string) => {
    return availableTargets.find((target) => target.modelId === targetModelId)?.label ?? targetModelId;
  };

  useEffect(() => {
    if (!pendingTargetId) {
      return;
    }

    const hasPendingTarget = remainingTargets.some((target) => target.modelId === pendingTargetId);
    if (!hasPendingTarget) {
      setPendingTargetId("");
    }
  }, [pendingTargetId, remainingTargets]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Route className="h-4 w-4" />
          {detailCopy.proxyTargets}
        </CardTitle>
        <CardDescription>
          {detailCopy.orderedPriorityRouting}
        </CardDescription>
        <p className="text-sm text-muted-foreground">
          {copy.proxyTargetsDescriptionPrimary}
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        {draftTargets.length === 0 ? (
          <div className="rounded-md border border-dashed border-border px-3 py-4 text-sm text-muted-foreground">
            {copy.noProxyTargetsSelected}
          </div>
        ) : (
          <div className="space-y-2">
            {draftTargets.map((target, index) => (
              <div key={target.target_model_id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{resolveTargetLabel(target.target_model_id)}</p>
                  <p className="text-xs text-muted-foreground">{copy.priority(formatNumber(index + 1))}</p>
                </div>
                <div className="flex items-center gap-1">
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    className="h-8 w-8"
                    aria-label={copy.targetMoveUp(target.target_model_id)}
                    disabled={index === 0}
                    onClick={() => setDraftTargets((current) => moveProxyTarget(current, index, index - 1))}
                  >
                    <ArrowUp className="h-4 w-4" />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    className="h-8 w-8"
                    aria-label={copy.targetMoveDown(target.target_model_id)}
                    disabled={index === draftTargets.length - 1}
                    onClick={() => setDraftTargets((current) => moveProxyTarget(current, index, index + 1))}
                  >
                    <ArrowDown className="h-4 w-4" />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    className="h-8 w-8"
                    aria-label={copy.targetRemove(target.target_model_id)}
                    onClick={() => setDraftTargets((current) => removeProxyTarget(current, target.target_model_id))}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <p className="text-xs text-muted-foreground">
            {remainingTargets.length === 0
              ? copy.allNativeModelsIncluded
              : copy.remainingNativeTargets(formatNumber(remainingTargets.length))}
          </p>
          <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
            <div className="flex min-w-0 flex-1 sm:min-w-72 sm:flex-none">
              <Select
                value={pendingTargetId}
                onValueChange={setPendingTargetId}
                disabled={remainingTargets.length === 0}
              >
                <SelectTrigger className="h-9 w-full min-w-0 rounded-r-none">
                  <SelectValue className="min-w-0" placeholder={detailCopy.modelIdLabel} />
                </SelectTrigger>
                <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                  {remainingTargets.map((target) => (
                    <SelectItem key={target.modelId} value={target.modelId}>
                      <span className="block whitespace-normal break-words pr-4 leading-5">
                        {target.label}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-9 rounded-l-none border-l-0"
                disabled={!pendingTargetId}
                onClick={() => {
                  setDraftTargets((current) => appendProxyTarget(current, pendingTargetId));
                  setPendingTargetId("");
                }}
              >
                {copy.addTarget}
              </Button>
            </div>
            <Button
              type="button"
              size="sm"
              disabled={saving}
              onClick={() => void onSave(normalizeProxyTargets(draftTargets))}
            >
              <Save className="mr-1.5 h-4 w-4" />
              {detailCopy.saveChanges}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
