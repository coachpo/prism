import { useEffect, useMemo, useState } from "react";
import { Cable, Copy, GitBranch, GripVertical, Loader2, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { LoadbalanceCurrentStateItem } from "@/lib/types/loadbalance";
import type {
  Connection,
  LegacyLoadbalanceStrategyType,
  ModelAccessTarget,
  ModelAccessTargetMutation,
  ModelConfigListItem,
  OpenAIImageCapability,
  OpenAITextCapability,
} from "@/lib/types";
import type {
  CurrentStateCompleteness,
  CurrentStateFailure,
  CurrentStateRowGap,
} from "@/pages/model-detail/useModelLoadbalanceCurrentState";
import { getTerminalTargetId, isTerminalTargetAccessTargetType } from "@/lib/types/target-compatibility";
import { cn, formatApiFamily } from "@/lib/utils";
import { useLocale } from "@/i18n/useLocale";
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system";
import { operationalRowActionsClassName } from "@/shared/table/operationalTable";
import { sortAccessTargetsByPositionThenId } from "./modelFormState";

const TARGET_COLUMN_COUNT = 8;

interface AccessTargetsEditorProps {
  accessTargets: ModelAccessTarget[];
  apiFamilyLabel: string;
  modelOptions: ModelConfigListItem[];
  connectionOptions?: Connection[];
  error?: string | null;
  disabled?: boolean;
  isConnectionTargetMutable?: (connectionId: number) => boolean;
  strategyType?: LegacyLoadbalanceStrategyType | null;
  currentStateByConnectionId?: Map<number, LoadbalanceCurrentStateItem>;
  /** Rows the cohort returned without a complete snapshot, and why. */
  currentStateGapByConnectionId?: Map<number, CurrentStateRowGap>;
  /** Set when the current-state read failed; never rendered as an empty cohort. */
  currentStateFailure?: CurrentStateFailure | null;
  /** Null until one successful read lands; `hasMore` means absence proves nothing. */
  currentStateCompleteness?: CurrentStateCompleteness | null;
  resettingConnectionIds?: Set<number> | number[];
  onResetCooldown?: (connectionId: number) => Promise<void> | void;
  onRefreshRuntimeState?: () => void;
  onAddTarget?: (target: ModelAccessTargetMutation) => Promise<void> | void;
  onCreateConnection?: () => void;
  onDeleteTarget?: (targetRowId: number) => Promise<void> | void;
  onEditConnection?: (connection: Connection) => void;
  onMoveTarget?: (targetRowId: number, toIndex: number) => Promise<void> | void;
  onToggleTarget?: (targetRowId: number, enabled: boolean) => Promise<void> | void;
  onCopyTarget?: (connection: Connection) => void;
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

function buildDraft(value: string, position: number): ModelAccessTargetMutation | null {
  const normalizedValue = value.trim();
  if (!normalizedValue) {
    return null;
  }

  return { target_type: "model", target_model_id: normalizedValue, position, is_enabled: true };
}

function textCapabilityLabel(
  capability: OpenAITextCapability | null | undefined,
  copy: ReturnType<typeof useLocale>["messages"]["modelsUi"],
): string | null {
  if (capability === "dual_native") return copy.terminalCapabilityDual;
  if (capability === "chat_completions_only") return copy.openaiAcceptedFormatChatCompletionsOnly;
  if (capability === "responses_only") return copy.terminalCapabilityResponses;
  return null;
}

function imageCapabilityLabel(
  capability: OpenAIImageCapability | null | undefined,
  copy: ReturnType<typeof useLocale>["messages"]["modelsUi"],
): string | null {
  if (capability === "generations") return copy.openaiImageOperationsGenerations;
  if (capability === "edits") return copy.openaiImageOperationsEdits;
  if (capability === "generations_and_edits") return copy.openaiImageOperationsGenerationsAndEdits;
  return null;
}

// A Terminal Target may declare a text capability, an image capability, or
// both. Reading only the text field renders an image-only target as a bare
// dash while the readiness card above says its image operations are routable.
function capabilityLabels(
  connection: Connection | null,
  copy: ReturnType<typeof useLocale>["messages"]["modelsUi"],
): string[] {
  return [
    textCapabilityLabel(connection?.openai_text_capability, copy),
    imageCapabilityLabel(connection?.openai_image_capability, copy),
  ].filter((label): label is string => Boolean(label));
}

export function AccessTargetsEditor({
  accessTargets,
  apiFamilyLabel,
  modelOptions,
  connectionOptions = [],
  error,
  disabled = false,
  isConnectionTargetMutable,
  strategyType = null,
  currentStateByConnectionId = new Map(),
  currentStateGapByConnectionId = new Map(),
  currentStateFailure = null,
  currentStateCompleteness = null,
  resettingConnectionIds,
  onResetCooldown,
  onRefreshRuntimeState,
  onAddTarget,
  onCreateConnection,
  onDeleteTarget,
  onEditConnection,
  onMoveTarget,
  onToggleTarget,
  onCopyTarget,
}: AccessTargetsEditorProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const connectionFallback = messages.modelDetailData.connectionFallback;

  const [pendingValue, setPendingValue] = useState("");
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [dragTargetId, setDragTargetId] = useState<number | null>(null);
  const [savingOrder, setSavingOrder] = useState(false);
  // Only the row currently being written is locked; the rest of the table
  // stays usable while a multi-row reorder commits.
  const [committingTargetId, setCommittingTargetId] = useState<number | null>(null);

  // The persisted mixed list is the authoritative editor state. Rows are sorted
  // defensively by (position ASC, id ASC); the server returns them in this
  // order after every mutation and after reloads.
  const persistedTargets = useMemo(() => sortAccessTargetsByPositionThenId(accessTargets), [accessTargets]);
  // Reordering is a draft until the operator commits it: dragging a handle
  // must not fire a request per hop, and a half-applied order is worse than
  // no reorder at all.
  const [draftOrder, setDraftOrder] = useState<number[] | null>(null);
  useEffect(() => {
    setDraftOrder(null);
  }, [persistedTargets]);

  const orderedTargets = useMemo(() => {
    if (!draftOrder) return persistedTargets;
    const byId = new Map(persistedTargets.map((target) => [target.id, target]));
    const reordered = draftOrder.map((id) => byId.get(id)).filter((target): target is ModelAccessTarget => Boolean(target));
    return reordered.length === persistedTargets.length ? reordered : persistedTargets;
  }, [draftOrder, persistedTargets]);

  const movedCount = draftOrder
    ? orderedTargets.filter((target, index) => persistedTargets[index]?.id !== target.id).length
    : 0;

  const selectedModelKeys = useMemo(
    () => new Set(
      persistedTargets
        .filter((target): target is typeof target & { target_model_id: string } =>
          target.target_type === "model" && Boolean(target.target_model_id?.trim()))
        .map((target) => target.target_model_id.trim()),
    ),
    [persistedTargets],
  );
  const remainingModels = modelOptions.filter((model) => !selectedModelKeys.has(model.model_id));
  const effectivePendingValue = remainingModels.some((model) => model.model_id === pendingValue) ? pendingValue : "";
  const hasBusyAction = busyKey !== null;
  const isResettingConnection = (connectionId: number) =>
    resettingConnectionIds instanceof Set
      ? resettingConnectionIds.has(connectionId)
      : resettingConnectionIds?.includes(connectionId) ?? false;
  const canMutate = Boolean(onMoveTarget || onToggleTarget || onDeleteTarget);
  const enabledTargetCount = persistedTargets.filter((target) => target.is_enabled !== false).length;
  const showSingleTruncationWarning = strategyType === "single" && enabledTargetCount >= 2;

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
    const draft = buildDraft(effectivePendingValue, persistedTargets.length);
    if (!draft || !onAddTarget) return;
    await runAction("add", () => onAddTarget(draft));
    setPendingValue("");
  };

  const handleDrop = (targetId: number) => {
    if (dragTargetId === null || dragTargetId === targetId) return;
    const currentIds = orderedTargets.map((target) => target.id);
    const from = currentIds.indexOf(dragTargetId);
    const to = currentIds.indexOf(targetId);
    if (from < 0 || to < 0) return;
    const next = [...currentIds];
    next.splice(from, 1);
    next.splice(to, 0, dragTargetId);
    setDraftOrder(next);
    setDragTargetId(null);
  };

  const commitOrder = async () => {
    if (!draftOrder || !onMoveTarget) return;
    setSavingOrder(true);
    try {
      for (const [index, target] of orderedTargets.entries()) {
        if (persistedTargets[index]?.id === target.id) continue;
        setCommittingTargetId(target.id);
        await onMoveTarget(target.id, index);
      }
      setDraftOrder(null);
    } finally {
      setCommittingTargetId(null);
      setSavingOrder(false);
    }
  };

  return (
    <OperatorSectionCard
      data-testid="access-targets-editor"
      title={copy.accessTargets}
      description={copy.accessTargetsDescription}
      actions={
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{copy.currentApiFamily(formatApiFamily(apiFamilyLabel))}</span>
          {onCreateConnection ? (
            <Button type="button" size="sm" variant="outline" onClick={onCreateConnection} disabled={disabled || hasBusyAction}>
              <Plus data-icon="inline-start" />
              {copy.newConnection}
            </Button>
          ) : null}
        </div>
      }
      contentClassName="flex flex-col gap-3"
    >
      {error ? <OperatorCallout intent="danger" description={error} data-testid="access-targets-error" /> : null}

      {showSingleTruncationWarning ? (
        <OperatorCallout
          intent="warning"
          title={messages.loadbalanceStrategyCopy.singleTruncationWarning(enabledTargetCount - 1)}
        >
          {messages.loadbalanceStrategyCopy.singleSummary}
        </OperatorCallout>
      ) : null}

      {persistedTargets.length === 0 ? (
        <OperatorEmptyState title={copy.noAccessTargetsSelected} className="py-6" />
      ) : (
        <>
          {/* 卡片自己的边框就是这张表的边框，不再套第二圈。 */}
          <div className="overflow-x-auto">
            <Table data-testid="access-targets-mixed-list">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-16">{copy.targetColumnPosition}</TableHead>
                  <TableHead>{copy.targetColumnType}</TableHead>
                  <TableHead>{copy.targetColumnName}</TableHead>
                  {/* DESIGN.md: a column whose values come from one basis says
                      so in the header. This column reads what each target
                      declares, never what the routing analyzer resolved. */}
                  <TableHead title={copy.targetColumnCapabilityBasis}>
                    <span className="inline-flex flex-wrap items-center gap-1">
                      {copy.targetColumnCapability}
                      <span aria-hidden="true" className="text-text-disabled">?</span>
                      <span className="sr-only">{copy.targetColumnCapabilityBasis}</span>
                    </span>
                  </TableHead>
                  <TableHead>{copy.targetColumnLimits}</TableHead>
                  <TableHead>{copy.targetColumnRuntime}</TableHead>
                  <TableHead>{copy.targetColumnEnabled}</TableHead>
                  <TableHead className="text-right">{copy.targetColumnActions}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {orderedTargets.map((target, targetIndex) => {
                  const isTerminalTarget = isTerminalTargetAccessTargetType(target.target_type);
                  const readOnlyConnection = isTerminalTarget && isReadOnlyConnection(target);
                  const positionLabel = formatNumber(targetIndex + 1);
                  const connectionId = isTerminalTarget ? getTerminalTargetId(target) : null;
                  const connection = isTerminalTarget
                    ? connectionOptions.find((candidate) => candidate.id === connectionId) ?? null
                    : null;
                  const canEditConnection = !readOnlyConnection && Boolean(connection && onEditConnection);
                  const name = isTerminalTarget
                    ? (connection ? getConnectionName(connection, connectionFallback) : connectionFallback(String(connectionId)))
                    : resolveModelTargetLabel(target.target_model_id?.trim() || "", modelOptions);
                  const runtime = connectionId != null ? currentStateByConnectionId.get(connectionId) : undefined;
                  const rowBusy = busyKey?.endsWith(`:${target.id}`) || committingTargetId === target.id;

                  return (
                    <TableRow
                      key={target.id}
                      className={cn("group/row", dragTargetId === target.id && "opacity-60")}
                      data-testid={`access-target-${target.id}`}
                      draggable={Boolean(onMoveTarget) && !disabled}
                      onDragStart={() => setDragTargetId(target.id)}
                      onDragOver={(event) => event.preventDefault()}
                      onDrop={() => handleDrop(target.id)}
                    >
                      <TableCell className="align-top">
                        <div className="flex items-center gap-1">
                          {onMoveTarget ? (
                            <span
                              aria-label={copy.targetDragHandle(name)}
                              className="cursor-grab text-muted-foreground"
                              role="button"
                              tabIndex={0}
                            >
                              <GripVertical className="size-4" />
                            </span>
                          ) : null}
                          <span className="font-mono text-xs tabular-nums">{positionLabel}</span>
                        </div>
                      </TableCell>

                      <TableCell className="align-top">
                        <span className="inline-flex items-center gap-1.5 text-xs">
                          {isTerminalTarget ? <Cable className="size-3.5" /> : <GitBranch className="size-3.5" />}
                          {isTerminalTarget ? copy.connectionTarget : copy.modelTarget}
                        </span>
                      </TableCell>

                      <TableCell className="align-top">
                        <div className="flex min-w-48 flex-col gap-0.5">
                          <span className="truncate text-sm font-medium" title={name}>{name}</span>
                          {connection?.endpoint?.base_url ? (
                            <span className="truncate font-mono text-xs text-muted-foreground" title={connection.endpoint.base_url}>
                              {connection.endpoint.base_url}
                            </span>
                          ) : null}
                          {connection?.is_active === false ? (
                            <OperatorTypeBadge intent="degraded" label={copy.connectionInactive} preserveLabel />
                          ) : null}
                        </div>
                      </TableCell>

                      <TableCell className="align-top">
                        <TargetCapability
                          apiFamilyLabel={apiFamilyLabel}
                          connection={connection}
                          copy={copy}
                          isTerminalTarget={isTerminalTarget}
                        />
                      </TableCell>

                      <TableCell className="align-top">
                        {isTerminalTarget ? (
                          <TargetLimits connection={connection} copy={copy} />
                        ) : (
                          // A Model Target holds no terminal configuration of
                          // its own — an em dash, not a zero. The reason says
                          // where the limits actually live; it does not claim
                          // that traffic through this row is unthrottled.
                          <OperatorMissingValue className="text-xs" reason={copy.targetLimitsNotApplicable} />
                        )}
                      </TableCell>

                      <TableCell className="align-top">
                        {isTerminalTarget ? (
                          <TargetRuntime
                            completeness={currentStateCompleteness}
                            connection={connection}
                            failure={currentStateFailure}
                            gap={connectionId != null ? currentStateGapByConnectionId.get(connectionId) : undefined}
                            rowParticipatesInRouting={target.is_enabled !== false}
                            resetting={connectionId != null && isResettingConnection(connectionId)}
                            state={runtime}
                            onReset={connectionId != null && onResetCooldown ? () => void onResetCooldown(connectionId) : undefined}
                          />
                        ) : (
                          <OperatorMissingValue className="text-xs" reason={copy.targetRuntimeNotApplicable} />
                        )}
                      </TableCell>

                      <TableCell className="align-top">
                        {!readOnlyConnection ? (
                          <Switch
                            checked={target.is_enabled !== false}
                            disabled={disabled || rowBusy}
                            onCheckedChange={(checked) => {
                              if (!onToggleTarget) return;
                              void runAction(`toggle:${target.id}`, () => onToggleTarget(target.id, checked));
                            }}
                            aria-label={copy.enableAccessTarget(positionLabel)}
                          />
                        ) : (
                          <OperatorStatusBadge
                            intent={target.is_enabled !== false ? "healthy" : "idle"}
                            preserveLabel
                            label={target.is_enabled !== false ? detailCopy.enabled : detailCopy.disabled}
                          />
                        )}
                      </TableCell>

                      <TableCell className="align-top text-right">
                        <div className={cn(operationalRowActionsClassName, "gap-1")}>
                          {canEditConnection ? (
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`${detailCopy.edit} ${name}`}
                              disabled={disabled || rowBusy}
                              onClick={() => onEditConnection?.(connection as Connection)}
                            >
                              <Pencil data-icon="inline-start" />
                              {detailCopy.edit}
                            </Button>
                          ) : null}
                          {!readOnlyConnection ? (
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="icon-sm"
                                  aria-label={copy.targetMoreActions(name)}
                                  disabled={disabled || rowBusy}
                                >
                                  <MoreHorizontal />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                {connection && onCopyTarget ? (
                                  <DropdownMenuItem
                                    data-testid="terminal-copy-action"
                                    onSelect={() => onCopyTarget(connection)}
                                  >
                                    <Copy />
                                    {copy.copyTargetAction(name)}
                                  </DropdownMenuItem>
                                ) : null}
                                {onRefreshRuntimeState ? (
                                  <DropdownMenuItem onSelect={() => onRefreshRuntimeState()}>
                                    {messages.routing.refresh}
                                  </DropdownMenuItem>
                                ) : null}
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  variant="destructive"
                                  onSelect={() => {
                                    if (!onDeleteTarget) return;
                                    void runAction(`delete:${target.id}`, () => onDeleteTarget(target.id));
                                  }}
                                >
                                  <Trash2 />
                                  {copy.targetRemove(positionLabel)}
                                </DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          ) : null}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>

          {movedCount > 0 ? (
            <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2 text-xs">
              <span className="mr-auto text-muted-foreground">{copy.targetOrderPending(formatNumber(movedCount))}</span>
              <Button type="button" variant="ghost" size="sm" disabled={savingOrder} onClick={() => setDraftOrder(null)}>
                {copy.targetOrderRevert}
              </Button>
              <Button type="button" size="sm" disabled={savingOrder} onClick={() => void commitOrder()}>
                {savingOrder ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
                {savingOrder ? copy.targetOrderSaving : copy.targetOrderSave}
              </Button>
            </div>
          ) : null}
        </>
      )}

      <div className="grid gap-3 rounded-lg border border-dashed bg-background p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
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
          disabled={disabled || hasBusyAction || !effectivePendingValue || !onAddTarget}
          onClick={() => void handleAdd()}
        >
          {busyKey === "add" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Plus data-icon="inline-start" />}
          {copy.addTarget}
        </Button>
      </div>

      {remainingModels.length === 0 ? (
        <p className="text-xs text-muted-foreground">{copy.noSameFamilyModelsAvailable}</p>
      ) : null}
    </OperatorSectionCard>
  );
}

/**
 * Capability is absent for four distinct reasons, and a bare dash tells the
 * operator none of them apart. Each branch carries the reason that actually
 * applies, so the same glyph never means four things at once.
 */
function TargetCapability({
  apiFamilyLabel,
  connection,
  copy,
  isTerminalTarget,
}: {
  apiFamilyLabel: string;
  connection: Connection | null;
  copy: ReturnType<typeof useLocale>["messages"]["modelsUi"];
  isTerminalTarget: boolean;
}) {
  if (!isTerminalTarget) {
    return <OperatorMissingValue className="text-xs" reason={copy.targetCapabilityNotApplicableModel} />;
  }
  if (!connection) {
    return <OperatorMissingValue className="text-xs" reason={copy.targetConnectionOutOfScope} />;
  }
  if (connection.api_family !== "openai") {
    return (
      <OperatorMissingValue
        className="text-xs"
        reason={copy.targetCapabilityNotApplicableFamily(formatApiFamily(apiFamilyLabel))}
      />
    );
  }
  const labels = capabilityLabels(connection, copy);
  if (labels.length === 0) {
    return <OperatorMissingValue className="text-xs" reason={copy.targetCapabilityUnknown} />;
  }
  return (
    <div className="flex flex-col items-start gap-1">
      {labels.map((label) => (
        <OperatorTypeBadge key={label} intent="accent" preserveLabel label={label} />
      ))}
    </div>
  );
}

function TargetLimits({
  connection,
  copy,
}: {
  connection: Connection | null;
  copy: ReturnType<typeof useLocale>["messages"]["modelsUi"];
}) {
  if (!connection) return <OperatorMissingValue className="text-xs" reason={copy.targetConnectionOutOfScope} />;
  const limits = [
    connection.qps_limit != null ? `QPS ${connection.qps_limit}` : null,
    connection.max_in_flight_non_stream != null ? `${copy.nonStreamShort} ${connection.max_in_flight_non_stream}` : null,
    connection.max_in_flight_stream != null ? `${copy.streamShort} ${connection.max_in_flight_stream}` : null,
  ].filter((value): value is string => Boolean(value));

  if (limits.length === 0) {
    return <span className="text-xs text-muted-foreground">{copy.terminalNoLimits}</span>;
  }
  return (
    <div className="flex min-w-0 flex-col gap-0.5 font-mono text-xs tabular-nums">
      {limits.map((limit) => <span key={limit}>{limit}</span>)}
    </div>
  );
}

/**
 * Runtime state collapses to one badge plus the numbers that matter: what is in
 * flight and how long the last success took. The full explanation lives in the
 * row's overflow menu and the routing-health page.
 *
 * Absence is never one thing. A read failure, a row the process has never
 * observed, a row observed only in part, a row excluded from the cohort because
 * it does not participate in routing, and a cohort cut short by paging are five
 * different facts — collapsing them into "本进程尚未观测" states a fact the page
 * has not established.
 */
function TargetRuntime({
  completeness,
  connection,
  failure,
  gap,
  onReset,
  resetting,
  rowParticipatesInRouting,
  state,
}: {
  completeness: CurrentStateCompleteness | null;
  connection: Connection | null;
  failure: CurrentStateFailure | null;
  gap: CurrentStateRowGap | undefined;
  onReset?: () => void;
  resetting: boolean;
  rowParticipatesInRouting: boolean;
  state: LoadbalanceCurrentStateItem | undefined;
}) {
  const { messages } = useLocale();
  const copy = messages.routing;

  if (!state) {
    // A failure with nothing previously on screen is a read failure, not an
    // observation. It outranks every "absent" reason below it.
    if (failure && !failure.staleData) {
      return (
        <OperatorStatusBadge
          intent="failing"
          preserveLabel
          label={copy.runtimeReadFailed}
          title={copy.runtimeReadFailedReason(failure.message)}
        />
      );
    }
    if (gap === "partial") {
      return (
        <OperatorClippedBadge
          label={copy.runtimePartialObservation}
          reason={copy.runtimePartialObservationReason}
        />
      );
    }
    if (gap === "unobserved") {
      return <span className="text-xs text-muted-foreground">{copy.noRuntimeObservation}</span>;
    }
    // Not in the cohort at all. The read model requires `is_enabled` on the
    // access-target edge, so a row switched off is out of scope rather than
    // unobserved.
    if (!rowParticipatesInRouting) {
      return <OperatorMissingValue className="text-xs" reason={copy.runtimeOutOfCohortReason} />;
    }
    if (completeness?.hasMore) {
      return <OperatorClippedBadge label={copy.runtimeTruncated} reason={copy.runtimeTruncatedReason} />;
    }
    if (!completeness) {
      // No successful read has landed yet; "never observed" is not proven.
      return <OperatorMissingValue className="text-xs" reason={copy.runtimeNotReadYet} />;
    }
    // The read succeeded, the row participates in routing and nothing was
    // truncated, yet the cohort does not contain this terminal target. That is
    // not the same fact as "the process has never observed it", so it must not
    // borrow that sentence.
    return <OperatorMissingValue className="text-xs" reason={copy.runtimeAbsentFromCohortReason} />;
  }

  const showReset = state.state === "retry_wait" || state.state === "banned";
  const latency = state.last_success_response_headers_latency_ms;
  // The runtime only increments these counters when the matching in-flight
  // limit is configured and positive, so an unlimited target reports a
  // permanent 0 that means "not metered", not "measured zero".
  const meteredNonStream = (connection?.max_in_flight_non_stream ?? 0) > 0;
  const meteredStream = (connection?.max_in_flight_stream ?? 0) > 0;

  return (
    <div className="flex min-w-0 flex-col items-start gap-1">
      <OperatorStatusBadge
        intent={state.state === "available" ? "healthy" : state.state === "banned" ? "failing" : "degraded"}
        preserveLabel
        label={
          state.state === "available"
            ? copy.noCooldown
            : state.state === "banned"
              ? copy.banned
              : copy.retryWait
        }
      />
      <span className="font-mono text-xs tabular-nums text-muted-foreground">
        {meteredNonStream ? (
          state.in_flight_non_stream
        ) : (
          <OperatorMissingValue className="text-xs" reason={copy.inFlightNotMeteredReason} />
        )}
        {" / "}
        {meteredStream ? (
          state.in_flight_stream
        ) : (
          <OperatorMissingValue className="text-xs" reason={copy.inFlightNotMeteredReason} />
        )}
        {latency != null ? ` · ${latency < 1000 ? `${latency} ms` : `${(latency / 1000).toFixed(2)} s`}` : ""}
      </span>
      {failure?.staleData ? (
        <OperatorStalenessBadge label={copy.stateStale} reason={failure.message} />
      ) : null}
      {showReset && onReset ? (
        <Button type="button" variant="outline" size="xs" disabled={resetting} onClick={onReset}>
          {resetting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
          {copy.resetCooldown}
        </Button>
      ) : null}
    </div>
  );
}

export { TARGET_COLUMN_COUNT };
