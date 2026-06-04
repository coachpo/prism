import { useMemo, useState } from "react";
import { Activity, ArrowDown, ArrowUp, Cable, GitBranch, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import {
  accessTargetKey,
  appendAccessTarget,
  getDefaultModelTargetTierSemantics,
  getIndexedConnectionAccessTargets,
  getModelAccessTargetTierGroups,
  getModelAccessTargetTierNumber,
  moveAccessTarget,
  normalizeAccessTargetMutations,
  parseModelAccessTargetTierNumberInput,
  parseModelTargetWeightInput,
  removeAccessTarget,
  setAccessTargetEnabled,
  updateModelAccessTargetTierSemantics,
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
  onUpdateModelTarget?: (
    index: number,
    updates: { weight: number; target_priority: number },
  ) => Promise<void> | void;
}

interface ModelTargetDraftState {
  target_priority: string;
  weight: string;
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
  draftPriority: string,
  draftWeight: string,
): ModelAccessTargetMutation | null {
  const normalizedValue = value.trim();
  const targetPriority = parseModelAccessTargetTierNumberInput(draftPriority);
  const weight = parseModelTargetWeightInput(draftWeight);
  if (!normalizedValue || targetPriority === null || weight === null) {
    return null;
  }

  return {
    target_type: "model",
    target_model_id: normalizedValue,
    position,
    target_priority: targetPriority,
    weight,
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
  onUpdateModelTarget,
}: AccessTargetsEditorProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const connectionFallback = messages.modelDetailData.connectionFallback;
  const defaultTierSemantics = getDefaultModelTargetTierSemantics();

  const [pendingValue, setPendingValue] = useState("");
  const [pendingPriority, setPendingPriority] = useState(String(getModelAccessTargetTierNumber(defaultTierSemantics.target_priority)));
  const [pendingWeight, setPendingWeight] = useState(String(defaultTierSemantics.weight));
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [modelTargetDrafts, setModelTargetDrafts] = useState<Record<string, ModelTargetDraftState>>({});

  const normalizedTargets = useMemo(() => normalizeAccessTargetMutations(accessTargets), [accessTargets]);
  const modelTierGroups = useMemo(() => getModelAccessTargetTierGroups(normalizedTargets), [normalizedTargets]);
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

  const clearModelTargetDraft = (draftKey: string) => {
    setModelTargetDrafts((current) => {
      if (!(draftKey in current)) {
        return current;
      }
      const next = { ...current };
      delete next[draftKey];
      return next;
    });
  };

  const updateModelTargetDraft = (
    draftKey: string,
    fallback: ModelTargetDraftState,
    field: keyof ModelTargetDraftState,
    value: string,
  ) => {
    setModelTargetDrafts((current) => ({
      ...current,
      [draftKey]: {
        ...(current[draftKey] ?? fallback),
        [field]: value,
      },
    }));
  };

  const handleAdd = async () => {
    const draft = buildDraft(effectivePendingValue, normalizedTargets.length, pendingPriority, pendingWeight);
    if (!draft) return;
    if (onAddTarget) {
      await runAction("add", () => onAddTarget(draft));
    } else {
      onChange(appendAccessTarget(normalizedTargets, draft));
    }
    setPendingValue("");
    setPendingPriority(String(getModelAccessTargetTierNumber(defaultTierSemantics.target_priority)));
    setPendingWeight(String(defaultTierSemantics.weight));
  };

  const handleModelTargetDraftCommit = async (
    sourceIndex: number,
    target: Extract<ModelAccessTargetMutation, { target_type: "model" }>,
  ) => {
    const draftKey = getModelDraftKey(target, sourceIndex);
    const fallback = {
      target_priority: String(getModelAccessTargetTierNumber(target.target_priority)),
      weight: String(target.weight ?? defaultTierSemantics.weight),
    } satisfies ModelTargetDraftState;
    const draft = modelTargetDrafts[draftKey] ?? fallback;
    const targetPriority = parseModelAccessTargetTierNumberInput(draft.target_priority);
    const weight = parseModelTargetWeightInput(draft.weight);

    if (targetPriority === null || weight === null) {
      clearModelTargetDraft(draftKey);
      return;
    }

    if (
      targetPriority === target.target_priority
      && weight === (target.weight ?? defaultTierSemantics.weight)
    ) {
      clearModelTargetDraft(draftKey);
      return;
    }

    const updates = { target_priority: targetPriority, weight };
    if (onUpdateModelTarget) {
      await runAction(`model-target:${sourceIndex}`, () => onUpdateModelTarget(sourceIndex, updates));
    } else {
      onChange(updateModelAccessTargetTierSemantics(normalizedTargets, sourceIndex, updates));
    }
    clearModelTargetDraft(draftKey);
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
      </div>

      {error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive" data-testid="access-targets-error">
          {error}
        </div>
      ) : null}

      <section className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{copy.modelFallbackTargets}</p>
          <p className="text-sm text-muted-foreground">{copy.modelFallbackTargetsDescription}</p>
        </div>

        {modelTierGroups.length === 0 ? (
          <div className="rounded-md border border-dashed border-border bg-background px-3 py-3 text-sm text-muted-foreground">
            {copy.noAccessTargetsSelected}
          </div>
        ) : null}

        {modelTierGroups.map((group) => (
          <div key={`tier:${group.target_priority}`} className="flex flex-col gap-3 rounded-xl border bg-background p-3">
            <div className="flex items-center gap-2">
              <Badge variant="outline">{copy.fallbackTier(formatNumber(getModelAccessTargetTierNumber(group.target_priority)))}</Badge>
              <p className="text-xs text-muted-foreground">{copy.modelTierDescription}</p>
            </div>

            <div className="flex flex-col gap-2">
              {group.targets.map(({ sourceIndex, target }, groupIndex) => {
                const targetKey = getModelDraftKey(target, sourceIndex);
                const draft = modelTargetDrafts[targetKey] ?? {
                  target_priority: String(getModelAccessTargetTierNumber(target.target_priority)),
                  weight: String(target.weight ?? defaultTierSemantics.weight),
                };
                const canMoveUp = groupIndex > 0;
                const canMoveDown = groupIndex < group.targets.length - 1;
                const previousSourceIndex = canMoveUp ? group.targets[groupIndex - 1]?.sourceIndex : null;
                const nextSourceIndex = canMoveDown ? group.targets[groupIndex + 1]?.sourceIndex : null;

                return (
                  <div
                    key={targetKey}
                    data-testid={`access-target-${targetKey}`}
                    className="flex flex-col gap-3 rounded-md border bg-muted/10 p-3"
                  >
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                      <div className="flex min-w-0 flex-1 items-start gap-3">
                        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-muted/30 text-muted-foreground">
                          <GitBranch className="h-4 w-4" />
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-sm font-medium">{resolveModelTargetLabel(target.target_model_id ?? "", modelOptions)}</p>
                          <p className="text-xs text-muted-foreground">
                            {copy.modelTarget}
                            {" · "}
                            {copy.fallbackTier(formatNumber(getModelAccessTargetTierNumber(target.target_priority)))}
                            {" · "}
                            {copy.weightValue(formatNumber(target.weight ?? defaultTierSemantics.weight))}
                            {target.is_enabled === false ? ` · ${detailCopy.disabled}` : ""}
                          </p>
                        </div>
                      </div>

                      <div className="grid gap-2 sm:grid-cols-[minmax(0,7rem)_minmax(0,7rem)_auto] sm:items-end">
                        <div className="grid gap-1">
                          <Label htmlFor={`model-target-tier-${sourceIndex}`} className="text-xs text-muted-foreground">{copy.tier}</Label>
                          <Input
                            id={`model-target-tier-${sourceIndex}`}
                            type="number"
                            min="1"
                            step="1"
                            inputMode="numeric"
                            value={draft.target_priority}
                            disabled={disabled || hasBusyAction}
                            aria-label={`${copy.tier} ${formatNumber(groupIndex + 1)}`}
                            onChange={(event) => updateModelTargetDraft(targetKey, draft, "target_priority", event.target.value)}
                            onBlur={() => void handleModelTargetDraftCommit(sourceIndex, target)}
                            onKeyDown={(event) => {
                              if (event.key === "Enter") {
                                event.currentTarget.blur();
                              }
                            }}
                          />
                        </div>

                        <div className="grid gap-1">
                          <Label htmlFor={`model-target-weight-${sourceIndex}`} className="text-xs text-muted-foreground">{copy.weight}</Label>
                          <Input
                            id={`model-target-weight-${sourceIndex}`}
                            type="number"
                            min="1"
                            step="1"
                            inputMode="numeric"
                            value={draft.weight}
                            disabled={disabled || hasBusyAction}
                            aria-label={`${copy.weight} ${formatNumber(groupIndex + 1)}`}
                            onChange={(event) => updateModelTargetDraft(targetKey, draft, "weight", event.target.value)}
                            onBlur={() => void handleModelTargetDraftCommit(sourceIndex, target)}
                            onKeyDown={(event) => {
                              if (event.key === "Enter") {
                                event.currentTarget.blur();
                              }
                            }}
                          />
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
                            aria-label={copy.enableAccessTarget(formatNumber(groupIndex + 1))}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={copy.targetMoveUp(formatNumber(groupIndex + 1))}
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
                            aria-label={copy.targetMoveDown(formatNumber(groupIndex + 1))}
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
                            aria-label={copy.targetRemove(formatNumber(groupIndex + 1))}
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
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        ))}

        <div className="grid gap-3 rounded-xl border border-dashed bg-background p-3 sm:grid-cols-[minmax(0,1fr)_7rem_7rem_auto] sm:items-end">
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

          <div className="grid gap-2">
            <Label htmlFor="access-target-tier">{copy.tier}</Label>
            <Input
              id="access-target-tier"
              type="number"
              min="1"
              step="1"
              inputMode="numeric"
              value={pendingPriority}
              disabled={disabled || remainingModels.length === 0}
              onChange={(event) => setPendingPriority(event.target.value)}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="access-target-weight">{copy.weight}</Label>
            <Input
              id="access-target-weight"
              type="number"
              min="1"
              step="1"
              inputMode="numeric"
              value={pendingWeight}
              disabled={disabled || remainingModels.length === 0}
              onChange={(event) => setPendingWeight(event.target.value)}
            />
          </div>

          <Button
            type="button"
            variant="outline"
            disabled={
              disabled
              || hasBusyAction
              || !effectivePendingValue
              || parseModelAccessTargetTierNumberInput(pendingPriority) === null
              || parseModelTargetWeightInput(pendingWeight) === null
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
              <div className="rounded-md border border-dashed border-border bg-background px-3 py-3 text-sm text-muted-foreground">
                {copy.noTerminalTargetsSelected}
              </div>
            ) : null}

            <div className="flex flex-col gap-2">
              {connectionTargets.map(({ sourceIndex, target }, connectionIndex) => {
                const targetKey = accessTargetKey(target) ?? `${target.target_type}:${sourceIndex}`;
                const connection = connectionOptions.find((candidate) => candidate.id === target.connection_id) ?? null;
                const isChecking = connection ? healthCheckingIds?.has(connection.id) ?? false : false;
                const isReadOnlyConnection = readOnlyConnectionIndexes.has(sourceIndex);
                const previousConnectionSourceIndex = connectionTargets[connectionIndex - 1]?.sourceIndex ?? null;
                const nextConnectionSourceIndex = connectionTargets[connectionIndex + 1]?.sourceIndex ?? null;
                const canMoveUp = previousConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(previousConnectionSourceIndex);
                const canMoveDown = nextConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(nextConnectionSourceIndex);
                const canEditConnection = !isReadOnlyConnection && Boolean(connection && (onHealthCheck || onEditConnection));

                return (
                  <div
                    key={targetKey}
                    data-testid={`access-target-${targetKey}`}
                    className="flex flex-col gap-3 rounded-md border bg-background px-3 py-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="flex min-w-0 flex-1 items-start gap-3">
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-muted/30 text-muted-foreground">
                        <Cable className="h-4 w-4" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">
                          {connection ? getConnectionName(connection, connectionFallback) : connectionFallback(String(target.connection_id))}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {copy.connectionTarget} · {copy.priority(formatNumber(connectionIndex + 1))}
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
    </div>
  );
}
