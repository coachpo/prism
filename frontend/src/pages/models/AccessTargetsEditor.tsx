import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Cable, GitBranch, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import type {
  Connection,
  ModelAccessTargetMutation,
  ModelConfigListItem,
} from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorEmptyState, OperatorInsetPanel, OperatorTypeBadge } from "@/shared/design-system";
import {
  accessTargetKey,
  appendAccessTarget,
  getIndexedConnectionAccessTargets,
  getIndexedModelAccessTargets,
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
  isConnectionTargetMutable?: (connectionId: number) => boolean;
  onAddTarget?: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onChange: (targets: ModelAccessTargetMutation[]) => void;
  onCreateConnection?: () => void;
  onDeleteTarget?: (index: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onMoveTarget?: (index: number, toIndex: number) => Promise<void> | void;
  onToggleTarget?: (index: number, enabled: boolean) => Promise<void> | void;
}

function getConnectionName(connection: Connection, connectionFallback: (id: string) => string) {
  return connection.name?.trim() || connection.endpoint?.name?.trim() || connectionFallback(String(connection.id));
}

function getModelLabel(model: ModelConfigListItem) {
  return model.display_name ? `${model.display_name} (${model.model_id})` : model.model_id;
}

function resolveModelTargetLabel(targetModelId: string, modelOptions: ModelConfigListItem[]) {
  return modelOptions.find((model) => model.model_id === targetModelId)?.display_name || targetModelId;
}

function buildDraft(
  value: string,
  position: number,
): ModelAccessTargetMutation | null {
  const normalizedValue = value.trim();
  if (!normalizedValue) {
    return null;
  }

  return {
    target_type: "model",
    target_model_id: normalizedValue,
    position,
    is_enabled: true,
  };
}

function getModelDraftKey(target: ModelAccessTargetMutation, sourceIndex: number) {
  return accessTargetKey(target) ?? `model:${sourceIndex}`;
}

export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions = [],
  error,
  disabled = false,
  isConnectionTargetMutable,
  onAddTarget,
  onChange,
  onCreateConnection,
  onDeleteTarget,
  onEditConnection,
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
  const modelTargets = useMemo(() => getIndexedModelAccessTargets(normalizedTargets), [normalizedTargets]);
  const connectionTargets = useMemo(() => getIndexedConnectionAccessTargets(normalizedTargets), [normalizedTargets]);
  const selectedKeys = useMemo(
    () => new Set(normalizedTargets.map(accessTargetKey).filter((key): key is string => key !== null)),
    [normalizedTargets],
  );
  const remainingModels = modelOptions.filter((model) => !selectedKeys.has(`model:${model.model_id}`));
  const effectivePendingValue = remainingModels.some((model) => model.model_id === pendingValue) ? pendingValue : "";
  const canManageConnectionTargets = Boolean(onDeleteTarget || onMoveTarget || onToggleTarget);
  const hasBusyAction = busyKey !== null;
  const showTerminalTargetSection = Boolean(onCreateConnection || connectionTargets.length > 0 || connectionOptions.length > 0);
  const readOnlyConnectionIndexes = useMemo(
    () => new Set(connectionTargets.flatMap(({ sourceIndex, target }) => {
      if (!canManageConnectionTargets) {
        return [sourceIndex];
      }
      return isConnectionTargetMutable?.(target.connection_id) === false ? [sourceIndex] : [];
    })),
    [canManageConnectionTargets, connectionTargets, isConnectionTargetMutable],
  );

  const runAction = async (key: string, action: () => Promise<void> | void) => {
    setBusyKey(key);
    try {
      await action();
    } finally {
      setBusyKey(null);
    }
  };

  const changeOrPersist = async (
    actionKey: string,
    action: () => ModelAccessTargetMutation[],
    persist?: () => Promise<void> | void,
  ) => {
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
    <OperatorInsetPanel data-testid="access-targets-editor">
      <div className="flex items-start gap-2">
        <GitBranch className="mt-0.5 size-4 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{copy.accessTargets}</p>
          <p className="text-sm text-muted-foreground">{copy.accessTargetsDescription}</p>
          <p className="text-xs text-muted-foreground">{copy.currentApiFamily(formatApiFamily(apiFamilyLabel))}</p>
        </div>
      </div>

      {error ? (
        <OperatorCallout intent="danger" description={error} data-testid="access-targets-error" />
      ) : null}

      <section className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{copy.modelFallbackTargets}</p>
          <p className="text-sm text-muted-foreground">{copy.modelFallbackTargetsDescription}</p>
        </div>

        {modelTargets.length === 0 ? (
          <OperatorEmptyState title={copy.noAccessTargetsSelected} className="py-6" />
        ) : null}

        <div className="flex flex-col gap-2">
          {modelTargets.map(({ sourceIndex, target }, modelTargetIndex) => {
            const targetKey = getModelDraftKey(target, sourceIndex);
            const canMoveUp = modelTargetIndex > 0;
            const canMoveDown = modelTargetIndex < modelTargets.length - 1;
            const previousSourceIndex = canMoveUp ? modelTargets[modelTargetIndex - 1]?.sourceIndex : null;
            const nextSourceIndex = canMoveDown ? modelTargets[modelTargetIndex + 1]?.sourceIndex : null;

            return (
              <div
                key={targetKey}
                data-testid={`access-target-${targetKey}`}
                className="flex flex-col gap-3 rounded-md border border-outline-variant bg-surface p-3 lg:flex-row lg:items-center lg:justify-between"
              >
                <div className="flex min-w-0 flex-1 items-start gap-3">
                  <div className="flex size-[var(--density-control-h-sm)] shrink-0 items-center justify-center rounded-md border border-outline-variant bg-surface-container-low text-muted-foreground">
                    <GitBranch />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{resolveModelTargetLabel(target.target_model_id ?? "", modelOptions)}</p>
                    <p className="text-xs text-muted-foreground">
                      {copy.modelTarget} · {copy.position(formatNumber(modelTargetIndex + 1))}
                      {target.is_enabled === false ? ` · ${detailCopy.disabled}` : ""}
                    </p>
                  </div>
                </div>

                <div className="flex flex-wrap items-center justify-end gap-2">
                          <Switch
                            checked={target.is_enabled !== false}
                            disabled={disabled || hasBusyAction}
                            onCheckedChange={(checked) => {
                              void changeOrPersist(
                                `toggle:${sourceIndex}`,
                                () => setAccessTargetEnabled(normalizedTargets, sourceIndex, checked),
                                onToggleTarget ? () => onToggleTarget(sourceIndex, checked) : undefined,
                              );
                            }}
                            aria-label={copy.enableAccessTarget(formatNumber(modelTargetIndex + 1))}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={copy.targetMoveUp(formatNumber(modelTargetIndex + 1))}
                            disabled={disabled || hasBusyAction || !canMoveUp || previousSourceIndex == null}
                            onClick={() => previousSourceIndex != null
                              ? void changeOrPersist(
                                `move:${sourceIndex}:up`,
                                () => moveAccessTarget(normalizedTargets, sourceIndex, previousSourceIndex),
                                onMoveTarget ? () => onMoveTarget(sourceIndex, previousSourceIndex) : undefined,
                              )
                              : undefined}
                          >
                            <ArrowUp />
                          </Button>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={copy.targetMoveDown(formatNumber(modelTargetIndex + 1))}
                            disabled={disabled || hasBusyAction || !canMoveDown || nextSourceIndex == null}
                            onClick={() => nextSourceIndex != null
                              ? void changeOrPersist(
                                `move:${sourceIndex}:down`,
                                () => moveAccessTarget(normalizedTargets, sourceIndex, nextSourceIndex),
                                onMoveTarget ? () => onMoveTarget(sourceIndex, nextSourceIndex) : undefined,
                              )
                              : undefined}
                          >
                            <ArrowDown />
                          </Button>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={copy.targetRemove(formatNumber(modelTargetIndex + 1))}
                            disabled={disabled || hasBusyAction}
                            onClick={() => void changeOrPersist(
                              `delete:${sourceIndex}`,
                              () => removeAccessTarget(normalizedTargets, sourceIndex),
                              onDeleteTarget ? () => onDeleteTarget(sourceIndex) : undefined,
                            )}
                          >
                            <Trash2 />
                          </Button>
                </div>
              </div>
            );
          })}
        </div>

        <div className="grid gap-3 rounded-xl border border-dashed bg-background p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
          <div className="grid gap-2">
            <Label htmlFor="access-target-select">{copy.selectSameFamilyModel}</Label>
            <Select value={pendingValue} onValueChange={setPendingValue} disabled={disabled || remainingModels.length === 0}>
              <SelectTrigger id="access-target-select" className="min-w-0">
                <SelectValue placeholder={copy.selectSameFamilyModel} />
              </SelectTrigger>
              <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                <SelectGroup>
                  {remainingModels.map((model) => (
                    <SelectItem key={model.id} value={model.model_id}>
                      <span className="block truncate">{getModelLabel(model)}</span>
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <Button
            type="button"
            variant="outline"
            disabled={
              disabled
              || hasBusyAction
              || !effectivePendingValue
            }
            onClick={() => void handleAdd()}
          >
            {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
            {copy.addTarget}
          </Button>
        </div>

        {remainingModels.length === 0 ? (
          <p className="text-xs text-muted-foreground">{copy.noSameFamilyModelsAvailable}</p>
        ) : null}
      </section>

      {showTerminalTargetSection ? (
        <>
          <Separator />
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{copy.terminalTargets}</p>
                <p className="text-sm text-muted-foreground">{copy.terminalTargetsDescription}</p>
              </div>
              {onCreateConnection ? (
                <Button type="button" size="sm" variant="outline" onClick={onCreateConnection}>
                  <Plus data-icon="inline-start" />
                  {copy.newConnection}
                </Button>
              ) : null}
            </div>

            {connectionTargets.length === 0 ? (
              <OperatorEmptyState title={copy.noTerminalTargetsSelected} className="py-6" />
            ) : null}

            <div className="flex flex-col gap-2">
              {connectionTargets.map(({ sourceIndex, target }, connectionIndex) => {
                const targetKey = accessTargetKey(target) ?? `${target.target_type}:${sourceIndex}`;
                const connection = connectionOptions.find((candidate) => candidate.id === target.connection_id) ?? null;
                const isReadOnlyConnection = readOnlyConnectionIndexes.has(sourceIndex);
                const previousConnectionSourceIndex = connectionTargets[connectionIndex - 1]?.sourceIndex ?? null;
                const nextConnectionSourceIndex = connectionTargets[connectionIndex + 1]?.sourceIndex ?? null;
                const canMoveUp = previousConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(previousConnectionSourceIndex);
                const canMoveDown = nextConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(nextConnectionSourceIndex);
                const canEditConnection = !isReadOnlyConnection && Boolean(connection && onEditConnection);

                return (
                  <div
                    key={targetKey}
                    data-testid={`access-target-${targetKey}`}
                    className="flex flex-col gap-3 rounded-md border bg-background px-3 py-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="flex min-w-0 flex-1 items-start gap-3">
                      <div className="flex size-[var(--density-control-h-sm)] shrink-0 items-center justify-center rounded-md border border-outline-variant bg-surface-container-low text-muted-foreground">
                        <Cable />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">
                          {connection ? getConnectionName(connection, connectionFallback) : connectionFallback(String(target.connection_id))}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {copy.connectionTarget} · {copy.priority(formatNumber(connectionIndex + 1))}
                          {target.is_enabled === false ? ` · ${detailCopy.disabled}` : ""}
                        </p>
                        {connection?.pricing_template_id === null ? (
                          <div className="mt-1">
                            <OperatorTypeBadge intent="warning" label={messages.pricing.connectionMissingTemplateBadge} preserveLabel />
                          </div>
                        ) : null}
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
                                  `toggle:${sourceIndex}`,
                                  () => setAccessTargetEnabled(normalizedTargets, sourceIndex, checked),
                                  onToggleTarget ? () => onToggleTarget(sourceIndex, checked) : undefined,
                                );
                              }}
                              aria-label={copy.enableAccessTarget(formatNumber(connectionIndex + 1))}
                            />
                            <Button
                              type="button"
                              variant="outline"
                              size="icon-sm"
                              aria-label={copy.targetMoveUp(formatNumber(connectionIndex + 1))}
                              disabled={disabled || hasBusyAction || !canMoveUp || previousConnectionSourceIndex == null}
                              onClick={() => previousConnectionSourceIndex != null
                                ? void changeOrPersist(
                                  `move:${sourceIndex}:up`,
                                  () => moveAccessTarget(normalizedTargets, sourceIndex, previousConnectionSourceIndex),
                                  onMoveTarget ? () => onMoveTarget(sourceIndex, previousConnectionSourceIndex) : undefined,
                                )
                                : undefined}
                            >
                              <ArrowUp />
                            </Button>
                            <Button
                              type="button"
                              variant="outline"
                              size="icon-sm"
                              aria-label={copy.targetMoveDown(formatNumber(connectionIndex + 1))}
                              disabled={disabled || hasBusyAction || !canMoveDown || nextConnectionSourceIndex == null}
                              onClick={() => nextConnectionSourceIndex != null
                                ? void changeOrPersist(
                                  `move:${sourceIndex}:down`,
                                  () => moveAccessTarget(normalizedTargets, sourceIndex, nextConnectionSourceIndex),
                                  onMoveTarget ? () => onMoveTarget(sourceIndex, nextConnectionSourceIndex) : undefined,
                                )
                                : undefined}
                            >
                              <ArrowDown />
                            </Button>
                          </>
                        ) : null}

                        {connection && onEditConnection ? (
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={`${detailCopy.edit} ${getConnectionName(connection, connectionFallback)}`}
                            disabled={disabled || hasBusyAction}
                            onClick={() => onEditConnection(connection)}
                          >
                            <Pencil />
                          </Button>
                        ) : null}

                        {!isReadOnlyConnection ? (
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={copy.targetRemove(formatNumber(connectionIndex + 1))}
                            disabled={disabled || hasBusyAction}
                            onClick={() => void changeOrPersist(
                              `delete:${sourceIndex}`,
                              () => removeAccessTarget(normalizedTargets, sourceIndex),
                              onDeleteTarget ? () => onDeleteTarget(sourceIndex) : undefined,
                            )}
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
          </section>
        </>
      ) : null}
    </OperatorInsetPanel>
  );
}
