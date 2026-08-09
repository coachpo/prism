import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Cable, GitBranch, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import type {
  Connection,
  ModelAccessTarget,
  ModelAccessTargetMutation,
  ModelConfigListItem,
} from "@/lib/types";
import { getTerminalTargetId, isTerminalTargetAccessTargetType } from "@/lib/types/target-compatibility";
import { formatApiFamily } from "@/lib/utils";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorEmptyState, OperatorInsetPanel, OperatorTypeBadge } from "@/shared/design-system";
import { sortAccessTargetsByPositionThenId } from "./modelFormState";

interface AccessTargetsEditorProps {
  accessTargets: ModelAccessTarget[];
  apiFamilyLabel: string;
  modelOptions: ModelConfigListItem[];
  connectionOptions?: Connection[];
  error?: string | null;
  disabled?: boolean;
  isConnectionTargetMutable?: (connectionId: number) => boolean;
  onAddTarget?: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onCreateConnection?: () => void;
  onDeleteTarget?: (targetRowId: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onMoveTarget?: (targetRowId: number, toIndex: number) => Promise<void> | void;
  onToggleTarget?: (targetRowId: number, enabled: boolean) => Promise<void> | void;
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

export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions = [],
  error,
  disabled = false,
  isConnectionTargetMutable,
  onAddTarget,
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

  // The persisted mixed list is the authoritative editor state. Rows are sorted
  // defensively by (position ASC, id ASC); the server returns them in this
  // order after every mutation and after reloads.
  const orderedTargets = useMemo(() => sortAccessTargetsByPositionThenId(accessTargets), [accessTargets]);
  const selectedModelKeys = useMemo(
    () => new Set(
      orderedTargets
        .filter((target): target is typeof target & { target_model_id: string } =>
          target.target_type === "model" && Boolean(target.target_model_id?.trim()))
        .map((target) => target.target_model_id.trim()),
    ),
    [orderedTargets],
  );
  const remainingModels = modelOptions.filter((model) => !selectedModelKeys.has(model.model_id));
  const effectivePendingValue = remainingModels.some((model) => model.model_id === pendingValue) ? pendingValue : "";
  const hasBusyAction = busyKey !== null;
  const canMutate = Boolean(onMoveTarget || onToggleTarget || onDeleteTarget);

  const isReadOnlyConnection = (target: ModelAccessTarget) => {
    if (!isTerminalTargetAccessTargetType(target.target_type)) {
      return false;
    }
    const connectionId = getTerminalTargetId(target);
    if (!canMutate || connectionId == null) {
      return true;
    }
    return isConnectionTargetMutable?.(connectionId) === false;
  };

  const runAction = async (key: string, action: () => Promise<void> | void) => {
    setBusyKey(key);
    try {
      await action();
    } finally {
      setBusyKey(null);
    }
  };

  const handleAdd = async () => {
    const draft = buildDraft(effectivePendingValue, orderedTargets.length);
    if (!draft || !onAddTarget) return;
    await runAction("add", () => onAddTarget(draft));
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

      <section className="flex flex-col gap-3" data-testid="access-targets-mixed-list">
        {orderedTargets.length === 0 ? (
          <OperatorEmptyState title={copy.noAccessTargetsSelected} className="py-6" />
        ) : null}

        <div className="flex flex-col gap-2">
          {orderedTargets.map((target, targetIndex) => {
            const isTerminalTarget = isTerminalTargetAccessTargetType(target.target_type);
            const readOnlyConnection = isTerminalTarget && isReadOnlyConnection(target);
            const canMoveUp = targetIndex > 0;
            const canMoveDown = targetIndex < orderedTargets.length - 1;
            const positionLabel = formatNumber(targetIndex + 1);
            const targetKey = `access-target-${target.id}`;
            const connection = isTerminalTarget
              ? connectionOptions.find((candidate) => candidate.id === getTerminalTargetId(target)) ?? null
              : null;
            const canEditConnection = !readOnlyConnection && Boolean(connection && onEditConnection);

            return (
              <div
                key={target.id}
                data-testid={targetKey}
                className="flex flex-col gap-3 rounded-md border border-outline-variant bg-surface p-3 lg:flex-row lg:items-center lg:justify-between"
              >
                <div className="flex min-w-0 flex-1 items-start gap-3">
                  <div className="flex size-[var(--density-control-h-sm)] shrink-0 items-center justify-center rounded-md border border-outline-variant bg-surface-container-low text-muted-foreground">
                    {isTerminalTarget ? <Cable /> : <GitBranch />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">
                      {isTerminalTarget
                        ? (connection ? getConnectionName(connection, connectionFallback) : connectionFallback(String(getTerminalTargetId(target))))
                        : resolveModelTargetLabel(target.target_model_id?.trim() || "", modelOptions)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {isTerminalTarget ? copy.connectionTarget : copy.modelTarget} · {copy.position(positionLabel)}
                      {target.is_enabled === false ? ` · ${detailCopy.disabled}` : ""}
                    </p>
                    {isTerminalTarget && connection?.pricing_template_id === null ? (
                      <div className="mt-1">
                        <OperatorTypeBadge intent="warning" label={messages.pricing.connectionMissingTemplateBadge} preserveLabel />
                      </div>
                    ) : null}
                  </div>
                </div>

                <div className="flex flex-wrap items-center justify-end gap-2">
                  {!readOnlyConnection ? (
                    <Switch
                      checked={target.is_enabled !== false}
                      disabled={disabled || hasBusyAction}
                      onCheckedChange={(checked) => {
                        if (!onToggleTarget) return;
                        void runAction(`toggle:${target.id}`, () => onToggleTarget(target.id, checked));
                      }}
                      aria-label={copy.enableAccessTarget(positionLabel)}
                    />
                  ) : null}
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label={copy.targetMoveUp(positionLabel)}
                    disabled={disabled || hasBusyAction || !canMoveUp}
                    onClick={() => {
                      if (!onMoveTarget || !canMoveUp) return;
                      void runAction(`move:${target.id}:up`, () => onMoveTarget(target.id, targetIndex - 1));
                    }}
                  >
                    <ArrowUp />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label={copy.targetMoveDown(positionLabel)}
                    disabled={disabled || hasBusyAction || !canMoveDown}
                    onClick={() => {
                      if (!onMoveTarget || !canMoveDown) return;
                      void runAction(`move:${target.id}:down`, () => onMoveTarget(target.id, targetIndex + 1));
                    }}
                  >
                    <ArrowDown />
                  </Button>
                  {!readOnlyConnection ? (
                    <>
                      {canEditConnection ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="icon-sm"
                          aria-label={`${detailCopy.edit} ${getConnectionName(connection as Connection, connectionFallback)}`}
                          disabled={disabled || hasBusyAction}
                          onClick={() => onEditConnection?.(connection as Connection)}
                        >
                          <Pencil />
                        </Button>
                      ) : null}
                      <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        aria-label={copy.targetRemove(positionLabel)}
                        disabled={disabled || hasBusyAction}
                        onClick={() => {
                          if (!onDeleteTarget) return;
                          void runAction(`delete:${target.id}`, () => onDeleteTarget(target.id));
                        }}
                      >
                        <Trash2 />
                      </Button>
                    </>
                  ) : null}
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
              || !onAddTarget
            }
            onClick={() => void handleAdd()}
          >
            {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
            {copy.addTarget}
          </Button>
        </div>

        {onCreateConnection ? (
          <div className="flex items-center justify-between gap-2 rounded-md border border-dashed bg-background px-3 py-2">
            <p className="text-sm text-muted-foreground">{copy.terminalTargets}</p>
            <Button type="button" size="sm" variant="outline" onClick={onCreateConnection} disabled={disabled || hasBusyAction}>
              <Plus data-icon="inline-start" />
              {copy.newConnection}
            </Button>
          </div>
        ) : null}

        {remainingModels.length === 0 ? (
          <p className="text-xs text-muted-foreground">{copy.noSameFamilyModelsAvailable}</p>
        ) : null}
      </section>
    </OperatorInsetPanel>
  );
}
