import { useEffect, useState } from "react";

import { Loader2, RefreshCw } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorHelpHint,
  OperatorInsetPanel,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorTypeBadge,
} from "@/shared/design-system";
import {
  OperationalTableSkeletonRows,
} from "@/shared/table/operationalTable";
import { PaginationLiveStatus } from "@/shared/table/paginationControls";
import {
  keepsCommittedRows,
  shouldShowPendingRows,
} from "@/shared/table/paginationStates";
import { GlobalCurrentStateRow } from "./GlobalCurrentStateRow";
import type { useRoutingHealthCurrentStateRead } from "./useRoutingHealthCurrentStateRead";
import type { useRoutingHealthCurrentStateReset } from "./useRoutingHealthCurrentStateReset";

type CurrentStateRead = ReturnType<typeof useRoutingHealthCurrentStateRead>;
type CurrentStateReset = ReturnType<typeof useRoutingHealthCurrentStateReset>;

export function GlobalCurrentStatePresentation({
  read,
  reset,
}: {
  read: CurrentStateRead;
  reset: CurrentStateReset;
}) {
  const { messages, formatNumber } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.routingHealth;
  const { cursor, cursorStack, fragment, goNextPage, goPreviousPage } = read;
  const [confirmTarget, setConfirmTarget] = useState<
    import("@/lib/types").GlobalCurrentStateItem | null
  >(null);
  const [modelDraft, setModelDraft] = useState(read.modelId ?? "");
  const rows = fragment.data?.items ?? [];
  const completeness = fragment.data?.completeness;
  const bannedCount = rows.filter(
    (item) =>
      item.observation_state === "observed" && item.state === "banned",
  ).length;
  const retryWaitCount = rows.filter(
    (item) =>
      item.observation_state === "observed" && item.state === "retry_wait",
  ).length;
  const unobservedRows = rows.filter(
    (item) => item.observation_state !== "observed",
  );
  const unobservedCount = unobservedRows.length;
  // 尚未观测的行整片都是同一个徽章加四个 —，信息量等于卡头那句摘要，却把真正
  // 有事实的行推出首屏，还各自占一个 tab 停点。默认折起来，展开与否记在本地。
  const [unobservedExpanded, setUnobservedExpanded] = useState(
    readUnobservedExpanded,
  );
  const observedRows = rows.filter(
    (item) => item.observation_state === "observed",
  );
  // 只有当还剩下有事实的行时才折叠；操作者专门筛「尚未观测」时不能把表清空。
  const foldsUnobserved = unobservedCount > 0 && observedRows.length > 0;
  const visibleRows =
    foldsUnobserved && !unobservedExpanded ? observedRows : rows;
  const showPendingRows = shouldShowPendingRows(fragment);
  const showCommittedRows = keepsCommittedRows(fragment) && rows.length > 0;
  const showTableShell = showPendingRows || showCommittedRows;
  const deepLinkedPage = Boolean(cursor) && cursorStack.length === 0;
  const liveMessage = !fragment.reading
    ? null
    : fragment.data === null
      ? messages.operationalTable.loadingFirstPage
      : deepLinkedPage
        ? messages.operationalTable.loadingTargetPage
        : messages.operationalTable.loadingPage(cursorStack.length + 1);
  const replaceFailureVisible =
    !fragment.reading &&
    fragment.error !== null &&
    fragment.data !== null &&
    !fragment.stale;

  useEffect(() => {
    // URL navigation can replace the model filter while the draft is open.
    // Keep the local input synchronized with that authoritative value.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setModelDraft(read.modelId ?? "");
  }, [read.modelId]);

  const submitModelFilter = () => {
    const next = modelDraft.trim() || undefined;
    if (next === read.modelId) return;
    read.updateSearch({ runtime_model_id: next });
  };

  return (
    <OperatorSectionCard
      data-testid="routing-health-current-state"
      title={copy.currentStateTitle}
      description={
        <>
          <span>{copy.currentStateDescription}</span>
          {rows.length > 0 ? (
            <span className="ml-1 text-foreground">
              {copy.currentStateSummary(
                formatNumber(rows.length),
                formatNumber(bannedCount),
                formatNumber(retryWaitCount),
              )}
              {unobservedCount > 0
                ? ` · ${copy.currentStateSummaryUnobserved(formatNumber(unobservedCount))}`
                : ""}
            </span>
          ) : null}
        </>
      }
      contentClassName="flex flex-col gap-3"
      actions={
        <div className="flex items-center gap-2">
          <PaginationLiveStatus message={liveMessage} />
          {fragment.stale && fragment.lastSuccessfulAt ? (
            <OperatorStalenessBadge
              label={messages.honesty.lastSuccessful(
                formatTime(fragment.lastSuccessfulAt),
              )}
              reason={fragment.error ?? undefined}
            />
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void read.load("refresh")}
            disabled={fragment.reading}
            aria-busy={fragment.reading}
          >
            {fragment.reading ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <RefreshCw data-icon="inline-start" />
            )}
            {copy.refresh}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
        <FieldGroup className="flex-1">
          <Field>
            <FieldLabel htmlFor="runtime-model-filter">
              {copy.modelFilterLabel}
            </FieldLabel>
            <Input
              id="runtime-model-filter"
              value={modelDraft}
              placeholder={copy.modelFilterSubmitPlaceholder}
              onChange={(event) => setModelDraft(event.target.value)}
              onBlur={submitModelFilter}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  submitModelFilter();
                }
              }}
            />
          </Field>
        </FieldGroup>
        <FieldGroup className="flex-1">
          <Field>
            <FieldLabel htmlFor="runtime-state-filter">
              {copy.stateFilterLabel}
            </FieldLabel>
            <Select
              value={read.states.length === 1 ? read.states[0] : "all"}
              onValueChange={(value) =>
                read.updateSearch({
                  runtime_state: value === "all" ? undefined : value,
                })
              }
            >
              <SelectTrigger id="runtime-state-filter" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="all">{copy.stateFilterAll}</SelectItem>
                  <SelectItem value="available">
                    {copy.stateAvailable}
                  </SelectItem>
                  <SelectItem value="retry_wait">
                    {copy.stateRetryWait}
                  </SelectItem>
                  <SelectItem value="banned">{copy.stateBanned}</SelectItem>
                  <SelectItem value="unobserved">
                    {copy.stateUnobserved}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>
      </div>

      {!fragment.reading &&
      fragment.error !== null &&
      fragment.data === null ? (
        <OperatorErrorState
          testId="runtime-load-error"
          title={copy.loadFailed}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() =>
                void read.load(fragment.data === null ? "initial" : "replace")
              }
            >
              <RefreshCw data-icon="inline-start" />
              {copy.retry}
            </Button>
          }
        />
      ) : null}

      {replaceFailureVisible ? (
        <OperatorErrorState
          testId="runtime-page-error"
          title={
            deepLinkedPage
              ? copy.loadFailed
              : messages.operationalTable.pageLoadFailed(cursorStack.length + 1)
          }
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void read.load("replace")}
            >
              <span data-icon="inline-start">↻</span>
              {copy.retry}
            </Button>
          }
        />
      ) : null}

      {completeness ? (
        <div
          className="flex flex-wrap items-center gap-2 text-xs"
          data-testid="runtime-completeness"
        >
          <OperatorTypeBadge
            label={completenessLabel(completeness.state, copy)}
            intent={completeness.state === "ready" ? "muted" : "degraded"}
            preserveLabel
          />
          <span className="text-muted-foreground">
            {copy.completenessCounts(
              formatNumber(completeness.observed_target_count),
              formatNumber(completeness.configured_target_count),
            )}
          </span>
          {completeness.state === "partial" ||
          completeness.state === "unobserved" ? (
            <OperatorClippedBadge
              label={copy.coverageIncompleteTitle}
              reason={copy.completenessPartialNote}
            />
          ) : null}
        </div>
      ) : null}

      {reset.resetError ? (
        <OperatorCallout intent="danger" title={copy.resetFailed} role="alert">
          {reset.resetError}
        </OperatorCallout>
      ) : null}
      {reset.resetNotice ? (
        <OperatorCallout intent="info" title={copy.resetNothingToClear}>
          {copy.resetNothingToClearDescription}
        </OperatorCallout>
      ) : null}

      {fragment.phase === "empty" && !fragment.reading ? (
        completeness?.state === "no_config" ? (
          <OperatorEmptyState
            title={copy.currentStateNoConfig}
            description={copy.currentStateNoConfigDescription}
          />
        ) : (
          <OperatorEmptyState
            title={copy.currentStateEmpty}
            description={copy.currentStateEmptyDescription}
          />
        )
      ) : null}

      {showTableShell ? (
        <div id="runtime-current-state-table" aria-busy={fragment.reading}>
          {/* sticky 表头只对最近的滚动祖先生效：Table 原语内部那层 overflow-x
              就是包含块，高度上限必须落在同一个容器上，横竖两轴才同源。 */}
          <Table
            aria-label={copy.currentStateTitle}
            scrollAreaClassName="max-h-[calc(100dvh-22rem)]"
          >
            <TableHeader>
              <TableRow>
                {/* 身份列与操作列冻结：这张表在 390 下要横滚三屏，
                    不冻结就分不清手里这一行是哪个模型配置，也够不到行尾。 */}
                <TableHead className="sticky left-0 z-20 bg-inset shadow-[inset_-1px_0_0_0_var(--color-border)]">
                  {copy.modelColumn}
                </TableHead>
                <TableHead>{copy.targetColumn}</TableHead>
                <TableHead>{copy.stateColumn}</TableHead>
                {/* 一个列头装了两个基准：斜杠两侧各自从哪儿起算要说出来。 */}
                <TableHead className="text-right">
                  <span className="inline-flex items-center gap-1">
                    {copy.attemptsColumn}
                    <OperatorHelpHint
                      label={copy.attemptsColumnHint}
                      align="end"
                    />
                  </span>
                </TableHead>
                <TableHead>{copy.nextRetryColumn}</TableHead>
                <TableHead>{copy.banUntilColumn}</TableHead>
                <TableHead className="sticky right-0 z-20 bg-inset text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
                  {copy.actionsColumn}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {showPendingRows ? (
                <OperationalTableSkeletonRows columns={7} rows={5} />
              ) : (
                visibleRows.map((item) => (
                  <GlobalCurrentStateRow
                    key={item.terminal_target.id}
                    item={item}
                    resetting={
                      reset.resettingTargetId === item.terminal_target.id
                    }
                    onRequestReset={() => setConfirmTarget(item)}
                    formatTime={formatTime}
                    formatNumber={formatNumber}
                    copy={copy}
                  />
                ))
              )}
            </TableBody>
          </Table>
        </div>
      ) : null}

      {showCommittedRows && foldsUnobserved ? (
        <OperatorInsetPanel
          data-testid="runtime-unobserved-fold"
          title={copy.currentStateSummaryUnobserved(
            formatNumber(unobservedCount),
          )}
          description={copy.unobservedFoldDescription}
          actions={
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-expanded={unobservedExpanded}
              aria-controls="runtime-current-state-table"
              onClick={() => {
                const next = !unobservedExpanded;
                setUnobservedExpanded(next);
                writeUnobservedExpanded(next);
              }}
            >
              {unobservedExpanded
                ? copy.unobservedFoldCollapse
                : copy.unobservedFoldExpand}
            </Button>
          }
        />
      ) : null}

      {cursorStack.length > 0 || fragment.data?.has_more || fragment.reading ? (
        <div className="flex items-center justify-end gap-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={fragment.reading || cursorStack.length === 0}
            onClick={goPreviousPage}
          >
            {copy.previousPage}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={
              fragment.reading ||
              !fragment.data?.has_more ||
              !fragment.data?.next_cursor
            }
            onClick={goNextPage}
          >
            {copy.nextPage}
          </Button>
        </div>
      ) : null}

      <AlertDialog
        open={confirmTarget !== null}
        onOpenChange={(open) => !open && setConfirmTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.resetCooldownConfirmTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              {copy.resetCooldownConfirmDescription(
                confirmTarget?.terminal_target.label ?? "",
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{copy.cancel}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = confirmTarget;
                setConfirmTarget(null);
                if (target) void reset.resetTarget(target.terminal_target.id);
              }}
            >
              {copy.resetCooldownConfirmAction}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </OperatorSectionCard>
  );
}

const UNOBSERVED_EXPANDED_STORAGE_KEY = "prism.routingHealth.unobservedExpanded";

function readUnobservedExpanded(): boolean {
  if (typeof window === "undefined") return false;
  return (
    window.localStorage?.getItem(UNOBSERVED_EXPANDED_STORAGE_KEY) === "true"
  );
}

function writeUnobservedExpanded(expanded: boolean): void {
  if (typeof window === "undefined") return;
  window.localStorage?.setItem(
    UNOBSERVED_EXPANDED_STORAGE_KEY,
    expanded ? "true" : "false",
  );
}

function completenessLabel(
  state: string,
  copy: ReturnType<typeof useLocale>["messages"]["routingHealth"],
): string {
  switch (state) {
    case "ready":
      return copy.completenessReady;
    case "no_config":
      return copy.completenessNoConfig;
    case "partial":
      return copy.completenessPartial;
    default:
      return copy.completenessUnobserved;
  }
}
