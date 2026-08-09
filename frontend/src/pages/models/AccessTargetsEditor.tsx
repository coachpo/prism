import { useMemo, useState } from "react";
import { GitBranch, Loader2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type {
  Connection,
  DiagnosticsTarget,
  LoadbalanceCurrentStateItem,
  ModelAccessTargetMutation,
  ModelConfigListItem,
  OpenAITextCapability,
  RoutingDiagnosticsResult,
} from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";
import { useLocale } from "@/i18n/useLocale";
import { OperatorEmptyState, OperatorInsetPanel } from "@/shared/design-system";
import {
  accessTargetKey,
  getIndexedConnectionAccessTargets,
  getIndexedModelAccessTargets,
  normalizeAccessTargetMutations,
} from "./modelFormState";
import { AccessTargetStageSection } from "@/pages/model-detail/AccessTargetStageSection";
import { ModelTargetRow } from "@/pages/model-detail/ModelTargetRow";
import { TerminalTargetCard } from "@/pages/model-detail/TerminalTargetCard";

interface AccessTargetsEditorProps {
  accessTargets: ModelAccessTargetMutation[];
  apiFamilyLabel: string;
  modelOptions: ModelConfigListItem[];
  connectionOptions?: Connection[];
  error?: string | null;
  disabled?: boolean;
  isConnectionTargetMutable?: (connectionId: number) => boolean;
  diagnostics?: RoutingDiagnosticsResult | null;
  modelConfigIDByModelID?: Map<string, number>;
  ownerOpenAIAcceptedFormat?: OpenAITextCapability | null;
  currentStateByConnectionId?: Map<number, LoadbalanceCurrentStateItem>;
  resettingConnectionIds?: Set<number>;
  onResetCooldown?: (connectionId: number) => void;
  onRefreshRuntimeState?: () => void;
  onAddTarget: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onCreateConnection?: () => void;
  onDeleteTarget: (index: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onCopyConnection?: (connection: Connection) => void;
  onQuickCapabilityChange?: (connection: Connection, capability: OpenAITextCapability) => void;
  onQuickPricingChange?: (connection: Connection, pricingTemplateId: number | null) => void;
  pricingTemplates?: Array<{ id: number; name: string }>;
  onMoveTarget: (index: number, toIndex: number) => Promise<void> | void;
  onToggleTarget: (index: number, enabled: boolean) => Promise<void> | void;
}

function buildDraft(value: string, position: number): ModelAccessTargetMutation | null {
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

function diagnosticsTargetsByConnectionID(diagnostics: RoutingDiagnosticsResult | null | undefined) {
  const byConnectionID = new Map<number, DiagnosticsTarget>();
  if (!diagnostics) return byConnectionID;
  for (const stage of diagnostics.stages) {
    if (stage.stage !== "terminal_targets") continue;
    for (const target of stage.targets) {
      if (target.connection_id != null) {
        byConnectionID.set(target.connection_id, target);
      }
    }
  }
  return byConnectionID;
}

function diagnosticsModelTargetsByConfigID(diagnostics: RoutingDiagnosticsResult | null | undefined) {
  const byConfigID = new Map<number, DiagnosticsTarget>();
  if (!diagnostics) return byConfigID;
  for (const stage of diagnostics.stages) {
    if (stage.stage !== "model_targets") continue;
    for (const target of stage.targets) {
      if (target.target_model_config_id != null) {
        byConfigID.set(target.target_model_config_id, target);
      }
    }
  }
  return byConfigID;
}

function stageTruncated(diagnostics: RoutingDiagnosticsResult | null | undefined, stage: "model_targets" | "terminal_targets") {
  if (!diagnostics || diagnostics.strategy.type !== "single") return false;
  const stageResult = diagnostics.stages.find((candidate) => candidate.stage === stage);
  if (!stageResult) return false;
  const enabledCount = stageResult.targets.filter((target) => target.enabled_strategy_index != null).length;
  return enabledCount > 1;
}

function rowTruncated(diagnostics: RoutingDiagnosticsResult | null | undefined, diagnosticsTarget: DiagnosticsTarget | null) {
  if (!diagnostics || diagnostics.strategy.type !== "single" || !diagnosticsTarget) return false;
  return diagnosticsTarget.enabled_strategy_index != null && diagnosticsTarget.enabled_strategy_index > 0;
}

// AccessTargetsEditor renders the two explicit routing stages: Model Targets
// first, Terminal Targets as fallback. Static coverage/warnings come from the
// backend diagnostics; runtime state comes from the process-local Ban Policy
// observation. The frontend never re-derives eligibility from card text.
export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions = [],
  error,
  disabled = false,
  isConnectionTargetMutable,
  diagnostics = null,
  modelConfigIDByModelID = new Map(),
  ownerOpenAIAcceptedFormat = null,
  currentStateByConnectionId = new Map(),
  resettingConnectionIds = new Set(),
  onResetCooldown,
  onRefreshRuntimeState,
  onAddTarget,
  onCreateConnection,
  onDeleteTarget,
  onEditConnection,
  onCopyConnection,
  onQuickCapabilityChange,
  onQuickPricingChange,
  pricingTemplates = [],
  onMoveTarget,
  onToggleTarget,
}: AccessTargetsEditorProps) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const routingCopy = messages.routing;

  const [pendingValue, setPendingValue] = useState("");
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const normalizedTargets = useMemo(() => normalizeAccessTargetMutations(accessTargets), [accessTargets]);
  const modelTargets = useMemo(() => getIndexedModelAccessTargets(normalizedTargets), [normalizedTargets]);
  const connectionTargets = useMemo(() => getIndexedConnectionAccessTargets(normalizedTargets), [normalizedTargets]);
  const diagnosticsTargetsByConnection = useMemo(() => diagnosticsTargetsByConnectionID(diagnostics), [diagnostics]);
  const diagnosticsModelTargets = useMemo(() => diagnosticsModelTargetsByConfigID(diagnostics), [diagnostics]);
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

  const handleAdd = async () => {
    const draft = buildDraft(effectivePendingValue, normalizedTargets.length);
    if (!draft) return;
    await runAction("add", () => onAddTarget(draft));
    setPendingValue("");
  };

  const modelStageTruncated = stageTruncated(diagnostics, "model_targets");
  const terminalStageTruncated = stageTruncated(diagnostics, "terminal_targets");

  return (
    <OperatorInsetPanel data-testid="access-targets-editor">
      <div className="flex items-start gap-2">
        <GitBranch className="mt-0.5 size-4 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm font-medium text-foreground">{copy.accessTargets}</p>
          <p className="text-sm text-muted-foreground">{routingCopy.modelStageDescription}</p>
          <p className="text-xs text-muted-foreground">{copy.currentApiFamily(formatApiFamily(apiFamilyLabel))}</p>
        </div>
      </div>

      {error ? (
        <p className="text-sm text-destructive" role="alert" data-testid="access-targets-error">
          {error}
        </p>
      ) : null}

      <AccessTargetStageSection stage="model_targets" truncatedBySingle={modelStageTruncated}>
        {modelTargets.map(({ sourceIndex, target }, modelTargetIndex) => {
          const targetKey = getModelDraftKey(target, sourceIndex);
          const canMoveUp = modelTargetIndex > 0;
          const canMoveDown = modelTargetIndex < modelTargets.length - 1;
          const previousSourceIndex = canMoveUp ? modelTargets[modelTargetIndex - 1]?.sourceIndex : null;
          const nextSourceIndex = canMoveDown ? modelTargets[modelTargetIndex + 1]?.sourceIndex : null;
          const modelConfigID = target.target_model_id != null ? modelConfigIDByModelID.get(target.target_model_id) : undefined;
          const diagnosticsTarget = modelConfigID != null ? diagnosticsModelTargets.get(modelConfigID) ?? null : null;

          return (
            <ModelTargetRow
              key={targetKey}
              stagePosition={modelTargetIndex + 1}
              target={target}
              diagnosticsTarget={diagnosticsTarget}
              modelOptions={modelOptions}
              truncatedBySingle={rowTruncated(diagnostics, diagnosticsTarget)}
              busy={hasBusyAction}
              disabled={disabled}
              canMoveUp={canMoveUp && previousSourceIndex != null}
              canMoveDown={canMoveDown && nextSourceIndex != null}
              onToggle={(checked) => {
                void runAction(`toggle:${sourceIndex}`, () => onToggleTarget(sourceIndex, checked));
              }}
              onMoveUp={() => previousSourceIndex != null && void runAction(`move:${sourceIndex}:up`, () => onMoveTarget(sourceIndex, previousSourceIndex))}
              onMoveDown={() => nextSourceIndex != null && void runAction(`move:${sourceIndex}:down`, () => onMoveTarget(sourceIndex, nextSourceIndex))}
              onDelete={() => void runAction(`delete:${sourceIndex}`, () => onDeleteTarget(sourceIndex))}
            />
          );
        })}
        {modelTargets.length === 0 ? (
          <OperatorEmptyState title={copy.noAccessTargetsSelected} className="py-6" />
        ) : null}
      </AccessTargetStageSection>

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
                    <span className="block truncate">{model.display_name ? `${model.display_name} (${model.model_id})` : model.model_id}</span>
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <Button
          type="button"
          variant="outline"
          disabled={disabled || hasBusyAction || !effectivePendingValue}
          onClick={() => void handleAdd()}
        >
          {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
          {copy.addTarget}
        </Button>
      </div>

      {remainingModels.length === 0 ? (
        <p className="text-xs text-muted-foreground">{copy.noSameFamilyModelsAvailable}</p>
      ) : null}

      {showTerminalTargetSection ? (
        <AccessTargetStageSection stage="terminal_targets" truncatedBySingle={terminalStageTruncated}>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <p className="text-sm font-medium text-foreground">{copy.terminalTargets}</p>
              <p className="text-sm text-muted-foreground">{routingCopy.terminalStageDescription}</p>
            </div>
            {onCreateConnection ? (
              <Button type="button" size="sm" variant="outline" onClick={onCreateConnection} data-testid="new-terminal-target-button">
                <Plus data-icon="inline-start" />
                {copy.newConnection}
              </Button>
            ) : null}
          </div>

          {connectionTargets.map(({ sourceIndex, target }, connectionIndex) => {
            const targetKey = accessTargetKey(target) ?? `${target.target_type}:${sourceIndex}`;
            const connection = connectionOptions.find((candidate) => candidate.id === target.connection_id) ?? null;
            const connectionId = target.connection_id ?? connection?.id;
            const isReadOnlyConnection = readOnlyConnectionIndexes.has(sourceIndex);
            const previousConnectionSourceIndex = connectionTargets[connectionIndex - 1]?.sourceIndex ?? null;
            const nextConnectionSourceIndex = connectionTargets[connectionIndex + 1]?.sourceIndex ?? null;
            const canMoveUp = previousConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(previousConnectionSourceIndex);
            const canMoveDown = nextConnectionSourceIndex != null && !readOnlyConnectionIndexes.has(nextConnectionSourceIndex);
            const canEditConnection = !isReadOnlyConnection && Boolean(connection && onEditConnection);
            const diagnosticsTarget = connectionId != null ? diagnosticsTargetsByConnection.get(connectionId) ?? null : null;
            const runtimeState = connectionId != null ? currentStateByConnectionId.get(connectionId) : undefined;
            const runtimeResetting = connectionId != null && resettingConnectionIds.has(connectionId);

            return (
              <TerminalTargetCard
                key={targetKey}
                stagePosition={connectionIndex + 1}
                target={target}
                connection={connection}
                diagnosticsTarget={diagnosticsTarget}
                truncatedBySingle={rowTruncated(diagnostics, diagnosticsTarget)}
                ownerOpenAIAcceptedFormat={ownerOpenAIAcceptedFormat}
                isReadOnly={isReadOnlyConnection && !canEditConnection}
                canMoveUp={canMoveUp}
                canMoveDown={canMoveDown}
                busy={hasBusyAction}
                disabled={disabled}
                runtimeState={runtimeState}
                runtimeResetting={runtimeResetting}
                onToggle={(checked) => {
                  void runAction(`toggle:${sourceIndex}`, () => onToggleTarget(sourceIndex, checked));
                }}
                onMoveUp={() => previousConnectionSourceIndex != null && void runAction(`move:${sourceIndex}:up`, () => onMoveTarget(sourceIndex, previousConnectionSourceIndex))}
                onMoveDown={() => nextConnectionSourceIndex != null && void runAction(`move:${sourceIndex}:down`, () => onMoveTarget(sourceIndex, nextConnectionSourceIndex))}
                onEdit={canEditConnection && connection && onEditConnection ? () => onEditConnection(connection) : undefined}
                onCopy={connection && onCopyConnection ? () => onCopyConnection(connection) : undefined}
                onQuickCapabilityChange={connection && onQuickCapabilityChange ? (capability) => onQuickCapabilityChange(connection, capability) : undefined}
                pricingTemplates={pricingTemplates}
                onQuickPricingChange={connection && onQuickPricingChange ? (pricingTemplateId) => onQuickPricingChange(connection, pricingTemplateId) : undefined}
                onDelete={() => void runAction(`delete:${sourceIndex}`, () => onDeleteTarget(sourceIndex))}
                onResetCooldown={onResetCooldown}
                onRefreshRuntime={onRefreshRuntimeState}
              />
            );
          })}
          {connectionTargets.length === 0 ? (
            <OperatorEmptyState title={copy.noTerminalTargetsSelected} className="py-6" />
          ) : null}
        </AccessTargetStageSection>
      ) : null}
    </OperatorInsetPanel>
  );
}
