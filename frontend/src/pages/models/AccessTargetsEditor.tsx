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

type TargetKind = "model" | "connection";

interface AccessTargetsEditorProps {
  accessTargets: ModelAccessTargetMutation[];
  apiFamilyLabel: string;
  modelOptions: ModelConfigListItem[];
  connectionOptions: Connection[];
  error?: string | null;
  disabled?: boolean;
  healthCheckingIds?: Set<number>;
  onAddTarget?: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onChange: (targets: ModelAccessTargetMutation[]) => void;
  onCreateConnection?: () => void;
  onDeleteTarget?: (index: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onHealthCheck?: (connectionId: number) => Promise<void> | void;
  onMoveTarget?: (index: number, toIndex: number) => Promise<void> | void;
  onToggleTarget?: (index: number, enabled: boolean) => Promise<void> | void;
}

function getConnectionName(connection: Connection) {
  return connection.name?.trim() || connection.endpoint?.name?.trim() || `Connection ${connection.id}`;
}

function getModelLabel(model: ModelConfigListItem) {
  return model.display_name ? `${model.display_name} (${model.model_id})` : model.model_id;
}

function resolveTargetLabel(
  target: ModelAccessTargetMutation,
  modelOptions: ModelConfigListItem[],
  connectionOptions: Connection[],
) {
  if (target.target_type === "model") {
    return modelOptions.find((model) => model.model_id === target.target_model_id)?.display_name || target.target_model_id;
  }
  const connection = connectionOptions.find((candidate) => candidate.id === target.connection_id);
  return connection ? getConnectionName(connection) : `Connection ${target.connection_id}`;
}

function buildDraft(kind: TargetKind, value: string, position: number): ModelAccessTargetMutation | null {
  if (kind === "model" && value.trim()) {
    return { target_type: "model", target_model_id: value.trim(), position, is_enabled: true };
  }

  const connectionId = Number.parseInt(value, 10);
  if (kind === "connection" && Number.isFinite(connectionId)) {
    return { target_type: "connection", connection_id: connectionId, position, is_enabled: true };
  }

  return null;
}

export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions,
  error,
  disabled = false,
  healthCheckingIds,
  onAddTarget,
  onChange,
  onCreateConnection,
  onDeleteTarget,
  onEditConnection,
  onHealthCheck,
  onMoveTarget,
  onToggleTarget,
}: AccessTargetsEditorProps) {
  const { formatNumber } = useLocale();
  const [pendingKind, setPendingKind] = useState<TargetKind>("connection");
  const [pendingValue, setPendingValue] = useState("");
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const normalizedTargets = useMemo(() => normalizeAccessTargetMutations(accessTargets), [accessTargets]);
  const selectedKeys = useMemo(
    () => new Set(normalizedTargets.map(accessTargetKey).filter((key): key is string => key !== null)),
    [normalizedTargets],
  );
  const remainingModels = modelOptions.filter((model) => !selectedKeys.has(`model:${model.model_id}`));
  const remainingConnections = connectionOptions.filter((connection) => !selectedKeys.has(`connection:${connection.id}`));
  const selectableValues = pendingKind === "model" ? remainingModels : remainingConnections;
  const effectivePendingValue = selectableValues.some((item) => String(pendingKind === "model" ? (item as ModelConfigListItem).model_id : (item as Connection).id) === pendingValue)
    ? pendingValue
    : "";

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
    const draft = buildDraft(pendingKind, effectivePendingValue, normalizedTargets.length);
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
          <p className="text-sm font-medium text-foreground">Access targets</p>
          <p className="text-sm text-muted-foreground">
            Select same-family models or standalone connections. Prism tries enabled rows in this order using the selected legacy strategy.
          </p>
          <p className="text-xs text-muted-foreground">Current API family: {formatApiFamily(apiFamilyLabel)}</p>
        </div>
        {onCreateConnection ? (
          <Button type="button" size="sm" variant="outline" onClick={onCreateConnection}>
            <Plus data-icon="inline-start" />
            New connection
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
          No access targets selected. This model can be saved disabled and have a target attached later. Enabled saves still require at least one enabled target.
        </div>
      ) : null}
      <div className="flex flex-col gap-2">
        {normalizedTargets.map((target, index) => {
          const targetKey = accessTargetKey(target) ?? `${target.target_type}:${index}`;
          const connection = target.target_type === "connection"
            ? connectionOptions.find((candidate) => candidate.id === target.connection_id)
            : null;
          const isChecking = connection ? healthCheckingIds?.has(connection.id) ?? false : false;
          return (
            <div key={targetKey} className="flex flex-col gap-3 rounded-md border bg-background px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 flex-1 items-start gap-3">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-muted/30 text-muted-foreground">
                  {target.target_type === "model" ? <GitBranch className="h-4 w-4" /> : <Cable className="h-4 w-4" />}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{resolveTargetLabel(target, modelOptions, connectionOptions)}</p>
                  <p className="text-xs text-muted-foreground">
                    {target.target_type === "model" ? "Model target" : "Connection target"} · Priority {formatNumber(index + 1)}
                    {target.is_enabled === false ? " · Disabled" : ""}
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <Switch
                  checked={target.is_enabled !== false}
                  disabled={disabled || busyKey === `toggle:${index}`}
                  onCheckedChange={(checked) => {
                    void changeOrPersist(
                      `toggle:${index}`,
                      () => setAccessTargetEnabled(normalizedTargets, index, checked),
                      onToggleTarget ? () => onToggleTarget(index, checked) : undefined,
                    );
                  }}
                  aria-label={`Enable access target ${index + 1}`}
                />
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={`Move target ${index + 1} up`}
                  disabled={disabled || index === 0 || busyKey === `move:${index}:up`}
                  onClick={() => void changeOrPersist(`move:${index}:up`, () => moveAccessTarget(normalizedTargets, index, index - 1), onMoveTarget ? () => onMoveTarget(index, index - 1) : undefined)}
                >
                  <ArrowUp />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={`Move target ${index + 1} down`}
                  disabled={disabled || index === normalizedTargets.length - 1 || busyKey === `move:${index}:down`}
                  onClick={() => void changeOrPersist(`move:${index}:down`, () => moveAccessTarget(normalizedTargets, index, index + 1), onMoveTarget ? () => onMoveTarget(index, index + 1) : undefined)}
                >
                  <ArrowDown />
                </Button>
                {connection && onHealthCheck ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label={`Health check ${getConnectionName(connection)}`}
                    disabled={disabled || isChecking}
                    onClick={() => void onHealthCheck(connection.id)}
                  >
                    {isChecking ? <Loader2 className="animate-spin" /> : <Activity />}
                  </Button>
                ) : null}
                {connection && onEditConnection ? (
                  <Button type="button" variant="outline" size="icon-sm" aria-label={`Edit ${getConnectionName(connection)}`} onClick={() => onEditConnection(connection)}>
                    <Pencil />
                  </Button>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label={`Remove target ${index + 1}`}
                  disabled={disabled || busyKey === `delete:${index}`}
                  onClick={() => void changeOrPersist(`delete:${index}`, () => removeAccessTarget(normalizedTargets, index), onDeleteTarget ? () => onDeleteTarget(index) : undefined)}
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
          );
        })}
      </div>

      <div className="grid gap-2 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-center">
        <Select value={pendingKind} onValueChange={(value) => {
          setPendingKind(value as TargetKind);
          setPendingValue("");
        }}>
          <SelectTrigger id="access-target-kind">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="connection">Connection</SelectItem>
            <SelectItem value="model">Model</SelectItem>
          </SelectContent>
        </Select>
        <Select value={pendingValue} onValueChange={setPendingValue} disabled={disabled || selectableValues.length === 0}>
          <SelectTrigger id="access-target-select" className="min-w-0">
            <SelectValue placeholder={pendingKind === "model" ? "Select same-family model" : "Select same-family connection"} />
          </SelectTrigger>
          <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
            {pendingKind === "model"
              ? remainingModels.map((model) => (
                  <SelectItem key={model.id} value={model.model_id}>
                    <span className="block truncate">{getModelLabel(model)}</span>
                  </SelectItem>
                ))
              : remainingConnections.map((connection) => (
                  <SelectItem key={connection.id} value={String(connection.id)}>
                    <span className="block truncate">{getConnectionName(connection)}</span>
                  </SelectItem>
                ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" disabled={disabled || !effectivePendingValue || busyKey === "add"} onClick={() => void handleAdd()}>
          {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
          Add target
        </Button>
      </div>

      {pendingKind === "connection" && remainingConnections.length === 0 ? (
        <p className="text-xs text-muted-foreground">No unattached same-family standalone connections are available. This model can be saved disabled and have a target attached later; enabled saves still require a target.</p>
      ) : null}
      {pendingKind === "model" && remainingModels.length === 0 ? (
        <p className="text-xs text-muted-foreground">No other same-family models are available. This model can be saved disabled and have a target attached later; enabled saves still require a target.</p>
      ) : null}
    </div>
  );
}
