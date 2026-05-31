import { useMemo, useState } from "react";
import { Activity, ArrowDown, ArrowUp, Cable, GitBranch, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type { Connection, ModelAccessTargetMutation, ModelConfigListItem } from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";
import { useLocale } from "@/i18n/useLocale";
import {
  accessTargetKey,
  appendAccessTarget,
  moveAccessTarget,
  normalizeAccessTargetMutations,
  removeAccessTarget,
  setAccessTargetEnabled,
} from "./modelFormState";

interface AccessTargetsEditorProps {
  accessTargets: ModelAccessTargetMutation[];
  apiFamilyLabel: string;
  modelOptions: ModelConfigListItem[];
  connectionOptions?: Connection[];
  error?: string | null;
  disabled?: boolean;
  healthCheckingIds?: Set<number>;
  isConnectionTargetMutable?: (connectionId: number) => boolean;
  onAddTarget?: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onChange: (targets: ModelAccessTargetMutation[]) => void;
  onCreateConnection?: () => void;
  onDeleteTarget?: (index: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onHealthCheck?: (connectionId: number) => Promise<void> | void;
  onMoveTarget?: (index: number, toIndex: number) => Promise<void> | void;
  onToggleTarget?: (index: number, enabled: boolean) => Promise<void> | void;
}

function getConnectionName(connection: Connection, connectionFallback: (id: string) => string) {
  return connection.name?.trim() || connection.endpoint?.name?.trim() || connectionFallback(String(connection.id));
}

function getModelLabel(model: ModelConfigListItem) {
  return model.display_name ? `${model.display_name} (${model.model_id})` : model.model_id;
}

function resolveTargetLabel(
  target: ModelAccessTargetMutation,
  modelOptions: ModelConfigListItem[],
  connectionOptions: Connection[],
  connectionFallback: (id: string) => string,
) {
  if (target.target_type === "model") {
    return modelOptions.find((model) => model.model_id === target.target_model_id)?.display_name || target.target_model_id;
  }
  const connection = connectionOptions.find((candidate) => candidate.id === target.connection_id);
  return connection ? getConnectionName(connection, connectionFallback) : connectionFallback(String(target.connection_id));
}

function buildDraft(value: string, position: number): ModelAccessTargetMutation | null {
  if (value.trim()) {
    return { target_type: "model", target_model_id: value.trim(), position, is_enabled: true };
  }

  return null;
}

export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions = [],
  error,
  disabled = false,
  healthCheckingIds,
  isConnectionTargetMutable,
  onAddTarget,
  onChange,
  onCreateConnection,
  onDeleteTarget,
  onEditConnection,
  onHealthCheck,
  onMoveTarget,
  onToggleTarget,
}: AccessTargetsEditorProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const connectionFallback = messages.modelDetailData.connectionFallback;
  const [pendingValue, setPendingValue] = useState("");
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const normalizedTargets = useMemo(() => normalizeAccessTargetMutations(accessTargets), [accessTargets]);
  const selectedKeys = useMemo(
    () => new Set(normalizedTargets.map(accessTargetKey).filter((key): key is string => key !== null)),
    [normalizedTargets],
  );
  const remainingModels = modelOptions.filter((model) => !selectedKeys.has(`model:${model.model_id}`));
  const effectivePendingValue = remainingModels.some((model) => model.model_id === pendingValue) ? pendingValue : "";
  const canManageConnectionTargets = Boolean(onDeleteTarget || onMoveTarget || onToggleTarget);
  const hasBusyAction = busyKey !== null;
  const readOnlyConnectionIndexes = useMemo(
    () => new Set(normalizedTargets.flatMap((target, index) => {
      if (target.target_type !== "connection") {
        return [];
      }
      if (!canManageConnectionTargets) {
        return [index];
      }
      return isConnectionTargetMutable?.(target.connection_id) === false ? [index] : [];
    })),
    [canManageConnectionTargets, isConnectionTargetMutable, normalizedTargets],
  );

  const runAction = async (key: string, action: () => Promise<void> | void) => {
    setBusyKey(key);
    try {
      await action();
    } finally {
      setBusyKey(null);
    }
  };

  const changeOrPersist = async (actionKey: string, action: () => ModelAccessTargetMutation[], persist?: () => Promise<void> | void) => {
    if (persist) {
      await runAction(actionKey, persist);
      return;
    }
    onChange(action());
  };
  const handleAdd = async () => {
    const draft = buildDraft(effectivePendingValue, normalizedTargets.length);
    if (!draft) return;
    if (onAddTarget) {
      await runAction("add", () => onAddTarget(draft));
    } else {
      onChange(appendAccessTarget(normalizedTargets, draft));
    }
    setPendingValue("");
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg border bg-muted/15 p-4" data-testid="access-targets-editor">
      <div className="flex items-start gap-2">
        <GitBranch className="mt-0.5 h-4 w-4 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{copy.accessTargets}</p>
          <p className="text-sm text-muted-foreground">{copy.accessTargetsDescription}</p>
          <p className="text-xs text-muted-foreground">{copy.currentApiFamily(formatApiFamily(apiFamilyLabel))}</p>
        </div>
        {onCreateConnection ? (
          <Button type="button" size="sm" variant="outline" onClick={onCreateConnection}>
            <Plus data-icon="inline-start" />
            {copy.newConnection}
          </Button>
        ) : null}
      </div>

      {error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive" data-testid="access-targets-error">
          {error}
        </div>
      ) : null}

      {normalizedTargets.length === 0 ? (
        <div className="rounded-md border border-dashed border-border bg-background px-3 py-3 text-sm text-muted-foreground">
          {copy.noAccessTargetsSelected}
        </div>
      ) : null}
      <div className="flex flex-col gap-2">
        {normalizedTargets.map((target, index) => {
          const targetKey = accessTargetKey(target) ?? `${target.target_type}:${index}`;
          const connection = target.target_type === "connection"
            ? connectionOptions.find((candidate) => candidate.id === target.connection_id)
            : null;
          const isChecking = connection ? healthCheckingIds?.has(connection.id) ?? false : false;
          const isReadOnlyConnection = readOnlyConnectionIndexes.has(index);
          const canMoveUp = index > 0 && !readOnlyConnectionIndexes.has(index - 1);
          const canMoveDown = index < normalizedTargets.length - 1 && !readOnlyConnectionIndexes.has(index + 1);
          const canEditConnection = !isReadOnlyConnection && Boolean(connection && (onHealthCheck || onEditConnection));
          return (
            <div key={targetKey} data-testid={`access-target-${targetKey}`} className="flex flex-col gap-3 rounded-md border bg-background px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 flex-1 items-start gap-3">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-muted/30 text-muted-foreground">
                  {target.target_type === "model" ? <GitBranch className="h-4 w-4" /> : <Cable className="h-4 w-4" />}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{resolveTargetLabel(target, modelOptions, connectionOptions, connectionFallback)}</p>
                  <p className="text-xs text-muted-foreground">
                    {target.target_type === "model" ? copy.modelTarget : copy.connectionTarget} · {copy.priority(formatNumber(index + 1))}
                    {target.is_enabled === false ? ` · ${detailCopy.disabled}` : ""}
                  </p>
                </div>
              </div>
              {!isReadOnlyConnection || canEditConnection ? (
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {!isReadOnlyConnection ? (
                    <>
                      <Switch
                        checked={target.is_enabled !== false}
                        disabled={disabled || hasBusyAction}
                        onCheckedChange={(checked) => {
                          void changeOrPersist(
                            `toggle:${index}`,
                            () => setAccessTargetEnabled(normalizedTargets, index, checked),
                            onToggleTarget ? () => onToggleTarget(index, checked) : undefined,
                          );
                        }}
                        aria-label={copy.enableAccessTarget(formatNumber(index + 1))}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        aria-label={copy.targetMoveUp(formatNumber(index + 1))}
                        disabled={disabled || hasBusyAction || !canMoveUp}
                        onClick={() => void changeOrPersist(`move:${index}:up`, () => moveAccessTarget(normalizedTargets, index, index - 1), onMoveTarget ? () => onMoveTarget(index, index - 1) : undefined)}
                      >
                        <ArrowUp />
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        aria-label={copy.targetMoveDown(formatNumber(index + 1))}
                        disabled={disabled || hasBusyAction || !canMoveDown}
                        onClick={() => void changeOrPersist(`move:${index}:down`, () => moveAccessTarget(normalizedTargets, index, index + 1), onMoveTarget ? () => onMoveTarget(index, index + 1) : undefined)}
                      >
                        <ArrowDown />
                      </Button>
                    </>
                  ) : null}
                  {connection && onHealthCheck ? (
                    <Button
                      type="button"
                      variant="outline"
                      size="icon-sm"
                      aria-label={`${detailCopy.healthCheck} ${getConnectionName(connection, connectionFallback)}`}
                      disabled={disabled || hasBusyAction || isChecking}
                      onClick={() => void onHealthCheck(connection.id)}
                    >
                      {isChecking ? <Loader2 className="animate-spin" /> : <Activity />}
                    </Button>
                  ) : null}
                  {connection && onEditConnection ? (
                    <Button type="button" variant="outline" size="icon-sm" aria-label={`${detailCopy.edit} ${getConnectionName(connection, connectionFallback)}`} disabled={disabled || hasBusyAction} onClick={() => onEditConnection(connection)}>
                      <Pencil />
                    </Button>
                  ) : null}
                  {!isReadOnlyConnection ? (
                    <Button
                      type="button"
                      variant="outline"
                      size="icon-sm"
                      aria-label={copy.targetRemove(formatNumber(index + 1))}
                      disabled={disabled || hasBusyAction}
                      onClick={() => void changeOrPersist(`delete:${index}`, () => removeAccessTarget(normalizedTargets, index), onDeleteTarget ? () => onDeleteTarget(index) : undefined)}
                    >
                      <Trash2 />
                    </Button>
                  ) : null}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>

      <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
        <Select value={pendingValue} onValueChange={setPendingValue} disabled={disabled || remainingModels.length === 0}>
          <SelectTrigger id="access-target-select" className="min-w-0">
            <SelectValue placeholder={copy.selectSameFamilyModel} />
          </SelectTrigger>
          <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
            {remainingModels.map((model) => (
              <SelectItem key={model.id} value={model.model_id}>
                <span className="block truncate">{getModelLabel(model)}</span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" disabled={disabled || hasBusyAction || !effectivePendingValue} onClick={() => void handleAdd()}>
          {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
          {copy.addTarget}
        </Button>
      </div>

      {remainingModels.length === 0 ? (
        <p className="text-xs text-muted-foreground">{copy.noSameFamilyModelsAvailable}</p>
      ) : null}
    </div>
  );
}
