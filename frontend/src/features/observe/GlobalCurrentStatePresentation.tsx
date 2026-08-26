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
  const unobservedCount = rows.filter(
    (item) => item.observation_state !== "observed",
  ).length;
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
        <div className="overflow-x-auto" aria-busy={fragment.reading}>
          <Table aria-label={copy.currentStateTitle}>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.modelColumn}</TableHead>
                <TableHead>{copy.targetColumn}</TableHead>
                <TableHead>{copy.stateColumn}</TableHead>
                <TableHead className="text-right">
                  {copy.attemptsColumn}
                </TableHead>
                <TableHead>{copy.nextRetryColumn}</TableHead>
                <TableHead>{copy.banUntilColumn}</TableHead>
                <TableHead className="text-right">
                  {copy.actionsColumn}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {showPendingRows ? (
                <OperationalTableSkeletonRows columns={7} rows={5} />
              ) : (
                rows.map((item) => (
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
                    missingLabel={messages.honesty.noValue}
                  />
                ))
              )}
            </TableBody>
          </Table>
        </div>
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
