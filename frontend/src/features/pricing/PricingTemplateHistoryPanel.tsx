import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type {
  PricingCard,
  PricingTemplate,
  PricingTemplateRevision,
  PricingTemplateWindow,
} from "@/lib/types";
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStalenessBadge,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import { RateCell } from "./PricingTemplateRatePanel";

function revisionStructure(revision: PricingTemplateRevision) {
  return JSON.stringify({
    kind: revision.template_kind,
    card: revision.card,
    base: revision.base_card,
    tier: revision.tier,
    peak: revision.peak_card,
    offpeak: revision.offpeak_card,
    schedule: revision.schedule,
  });
}

function cardEntries(
  revision: PricingTemplateRevision,
): Array<[string, PricingCard | null | undefined]> {
  switch (revision.template_kind) {
    case "standard":
      return [["standard", revision.card]];
    case "tiered":
      return [
        ["tier_base", revision.base_card],
        ["tier_above", revision.tier?.card],
      ];
    case "peak_valley":
      return [
        ["peak", revision.peak_card],
        ["offpeak", revision.offpeak_card],
      ];
    default:
      return [];
  }
}

function formatMinute(value: number) {
  const minute = ((value % 1440) + 1440) % 1440;
  return `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
}

function formatWindow(window: PricingTemplateWindow, weekdayLabels: string[]) {
  const days = weekdayLabels
    .filter((_, bit) => (window.weekday_mask & (1 << bit)) !== 0)
    .join("、");
  return `${days || "—"} ${formatMinute(window.start_minute)}–${formatMinute(window.end_minute)}`;
}

function cardRoleLabel(
  role: string,
  copy: ReturnType<typeof useLocale>["messages"]["pricingTemplatesUi"],
) {
  switch (role) {
    case "standard":
      return copy.cardStandard;
    case "tier_base":
      return copy.cardTierBase;
    case "tier_above":
      return copy.cardTierAbove;
    case "peak":
      return copy.cardPeak;
    case "offpeak":
      return copy.cardOffpeak;
    default:
      return role;
  }
}

function revisionSourceLabel(
  history: ReturnType<typeof useLocale>["messages"]["pricingTemplatesHistory"],
  source: PricingTemplateRevision["revision_source"],
): string {
  if (source === "catalog") return history.revisionSourceCatalog;
  if (source === "manual") return history.revisionSourceManual;
  return history.revisionSourceUnknown(source);
}

/**
 * 每条修订都带自己的 currency_code：历史行必须按修订当时的货币标注，
 * 不能借模板当前的符号。货币换过的旧修订退回三位代码，而不是无单位裸数字。
 */
function revisionCurrencySymbol(
  revision: PricingTemplateRevision,
  template: PricingTemplate,
) {
  return revision.currency_code === template.pricing_currency_code
    ? template.active_currency_symbol
    : revision.currency_code;
}

function RevisionEvidence({
  revision,
  symbol,
}: {
  revision: PricingTemplateRevision;
  symbol: string;
}) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const history = messages.pricingTemplatesHistory;
  const fields: Array<[keyof PricingCard, string, boolean]> = [
    ["input_price", copy.rateInput, false],
    ["output_price", copy.rateOutput, false],
    ["cached_input_price", copy.rateCachedInput, true],
    ["cache_creation_price", copy.rateCacheCreation, true],
    ["reasoning_price", copy.rateReasoning, true],
  ];
  return (
    <div className="flex min-w-[32rem] flex-col gap-2 py-1">
      {cardEntries(revision).map(([role, card]) => (
        <div key={role} className="flex flex-col gap-1">
          <OperatorValueBadge
            label={cardRoleLabel(role, copy)}
            className="w-fit text-[11px]"
          />
          {card ? (
            <div className="grid gap-2 sm:grid-cols-5">
              {fields.map(([field, label, specialty]) => (
                <div key={field}>
                  <p className="text-[10px] text-muted-foreground">{label}</p>
                  <RateCell
                    value={card[field]}
                    specialty={specialty}
                    symbol={symbol}
                  />
                </div>
              ))}
            </div>
          ) : (
            <OperatorMissingValue reason={messages.honesty.noValue} />
          )}
        </div>
      ))}
      {/* Append-only provenance: which source authored this revision, and the
          models.dev revision a catalog import replayed against. */}
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span>
          {history.revisionSourceLabel}:{" "}
          {revisionSourceLabel(history, revision.revision_source)}
        </span>
        {revision.catalog_revision ? (
          <span className="font-mono" title={revision.catalog_revision}>
            {history.revisionCatalogRevisionLabel}: {revision.catalog_revision}
          </span>
        ) : null}
      </div>
      {revision.template_kind === "peak_valley" ? (
        <div className="text-xs text-muted-foreground">
          <span>{revision.schedule?.timezone ?? "—"}</span>
          {revision.schedule?.windows?.length ? (
            <ul className="mt-1 list-inside list-disc">
              {revision.schedule.windows.map((window, index) => (
                <li
                  key={`${index}-${window.weekday_mask}-${window.start_minute}`}
                >
                  {formatWindow(
                    window,
                    messages.pricingTemplateDialog.weekdayLabels,
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <span className="ml-2">{history.scheduleUnavailable}</span>
          )}
        </div>
      ) : null}
    </div>
  );
}

export function PricingTemplateHistoryPanel({
  error,
  loading,
  revisions,
  onRetry,
  template,
}: {
  error: string | null;
  loading: boolean;
  revisions: PricingTemplateRevision[];
  onRetry: () => void;
  template: PricingTemplate;
}) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.pricingTemplatesUi;
  const history = messages.pricingTemplatesHistory;
  if (loading && revisions.length === 0)
    return <Skeleton className="h-20 rounded-md" />;
  if (error && revisions.length === 0)
    return (
      <OperatorErrorState
        title={messages.pricingTemplatesData.historyLoadFailed}
        description={error}
        action={
          <OperatorRetryButton onClick={onRetry}>
            {messages.common.retry}
          </OperatorRetryButton>
        }
      />
    );
  if (revisions.length === 0)
    return (
      <OperatorInsetPanel>
        <p className="text-xs text-muted-foreground">{history.empty}</p>
      </OperatorInsetPanel>
    );
  return (
    <OperatorInsetPanel className="p-0">
      {error ? (
        <OperatorStalenessBadge
          className="m-3"
          label={messages.pricingTemplatesData.historyLoadFailed}
          reason={error}
        />
      ) : null}
      {/* Table 自带 overflow-x-auto 容器；再套一层只会让两个滚动条谁都滚不动。 */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.columnVersion}</TableHead>
            <TableHead>{history.tableKind}</TableHead>
            <TableHead>{history.tableCards}</TableHead>
            <TableHead>{history.effectiveAt}</TableHead>
            <TableHead>{history.createdBy}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {revisions.map((revision, index) => {
            // 接口按 version 升序返回，上一版在前一行。取 index + 1 会把
            // 结构变更整体归到更旧的那一版上，等于指认错误的责任版本。
            const previous = revisions[index - 1];
            const changed = Boolean(
              previous &&
                revisionStructure(revision) !== revisionStructure(previous),
            );
            const isCurrent = revision.version === template.version;
            const cardCount = cardEntries(revision).filter(([, card]) =>
              Boolean(card),
            ).length;
            const kind =
              revision.template_kind === "standard"
                ? copy.kindStandard
                : revision.template_kind === "tiered"
                  ? copy.kindTiered
                  : revision.template_kind === "peak_valley"
                    ? copy.kindPeakValley
                    : revision.template_kind;
            return (
              <TableRow key={revision.revision_id}>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-1">
                    <OperatorValueBadge
                      label={`v${revision.version}`}
                      className="text-xs"
                    />
                    {isCurrent ? (
                      <OperatorTypeBadge
                        intent="accent"
                        preserveLabel
                        label={history.currentVersion}
                        title={history.currentVersionReason}
                      />
                    ) : null}
                  </div>
                </TableCell>
                <TableCell>
                  <OperatorValueBadge label={kind} className="text-xs" />
                </TableCell>
                <TableCell className="text-xs">
                  {copy.multiCardSummary(cardCount)}
                  {changed ? (
                    <span className="ml-2 text-failing">
                      {history.structureChanged}
                    </span>
                  ) : null}
                  <RevisionEvidence
                    revision={revision}
                    symbol={revisionCurrencySymbol(revision, template)}
                  />
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums">
                  {revision.effective_at ? (
                    formatTime(revision.effective_at)
                  ) : (
                    <OperatorMissingValue className="text-xs" />
                  )}
                </TableCell>
                <TableCell className="text-xs">
                  {history.createdByKind(revision.created_by_kind)}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </OperatorInsetPanel>
  );
}
