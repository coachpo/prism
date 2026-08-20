import { Link } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import type {
  PricingCard,
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
  PricingTemplateRevision,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStatusBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import { normalizeTemplatePrice } from "./pricingSchemas";

function RateCell({
  symbol,
  value,
  specialty,
}: {
  symbol?: string;
  value: string | null | undefined;
  specialty?: boolean;
}) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const normalized = normalizeTemplatePrice(value);

  if (normalized === "") {
    if (!specialty) return <OperatorMissingValue className="text-xs" />;
    return (
      <span className="inline-flex items-center justify-end gap-1">
        <OperatorMissingValue
          className="text-xs"
          reason={copy.rateUnconfiguredReason}
        />
        <OperatorStatusBadge
          intent="idle"
          preserveLabel
          label={copy.rateUnconfigured}
        />
      </span>
    );
  }

  return (
    <span className="font-mono text-xs tabular-nums">
      {/* A one-character symbol hugs the number ($1.50); a currency code needs
          a gap so it does not read as part of the digits (USD 1.50). */}
      {symbol ? (
        <span
          className={cn("text-muted-foreground", symbol.length > 1 && "mr-1")}
        >
          {symbol}
        </span>
      ) : null}
      {normalized}
    </span>
  );
}
function UsagePanel({
  error,
  loading,
  onRetry,
  rows,
}: {
  error: string | null;
  loading: boolean;
  onRetry: () => void;
  rows: PricingTemplateConnectionUsageItem[];
}) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;

  if (loading) return <Skeleton className="h-20 rounded-md" />;
  if (error) {
    return (
      <OperatorErrorState
        title={messages.pricingTemplatesData.loadUsageFailed}
        description={error}
        action={
          <OperatorRetryButton onClick={onRetry}>
            {messages.common.retry}
          </OperatorRetryButton>
        }
      />
    );
  }
  if (rows.length === 0) {
    return (
      <OperatorInsetPanel>
        <p className="text-xs text-muted-foreground">{copy.templateUnused}</p>
      </OperatorInsetPanel>
    );
  }

  return (
    <OperatorInsetPanel className="p-0">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.model}</TableHead>
            <TableHead>{copy.endpoint}</TableHead>
            <TableHead>{copy.terminalTargetColumn}</TableHead>
            <TableHead className="text-right">{copy.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={`${row.connection_id}-${row.model_config_id}`}>
              <TableCell>
                <Link
                  to="/route/models/$modelId"
                  params={{ modelId: String(row.model_config_id) }}
                  aria-label={copy.openModel(row.model_id)}
                  className="font-mono text-xs underline-offset-2 hover:underline"
                >
                  {row.model_id}
                </Link>
              </TableCell>
              <TableCell>
                <Link
                  to="/route/endpoints"
                  aria-label={copy.openEndpoint(row.endpoint_name)}
                  className="text-xs underline-offset-2 hover:underline"
                >
                  {row.endpoint_name}
                </Link>
              </TableCell>
              <TableCell className="text-xs">
                {row.connection_name ?? copy.unnamed}
              </TableCell>
              <TableCell className="text-right">
                <Button asChild type="button" variant="outline" size="sm">
                  <Link
                    to="/route/models/$modelId"
                    params={{ modelId: String(row.model_config_id) }}
                  >
                    {copy.rebindToOtherTemplate}
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperatorInsetPanel>
  );
}

function cardRoleLabel(
  role: string,
  copy: ReturnType<typeof useLocale>["messages"]["pricingTemplatesUi"],
): string {
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
      return copy.multiCardSummary(1);
  }
}

function CardRateGrid({ card, symbol }: { card: PricingCard; symbol: string }) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const fields: Array<[keyof PricingCard, string, boolean]> = [
    ["input_price", copy.rateInput, false],
    ["output_price", copy.rateOutput, false],
    ["cached_input_price", copy.rateCachedInput, true],
    ["cache_creation_price", copy.rateCacheCreation, true],
    ["reasoning_price", copy.rateReasoning, true],
  ];
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
      {fields.map(([field, label, specialty]) => (
        <div key={field}>
          <p className="text-xs text-muted-foreground">{label}</p>
          <RateCell specialty={specialty} symbol={symbol} value={card[field]} />
        </div>
      ))}
    </div>
  );
}

function TierPanel({ template }: { template: PricingTemplate }) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const templateDialogCopy = messages.pricingTemplateDialog;
  const cards: Array<{ role: string; card: PricingCard | null | undefined }> =
    template.template_kind === "standard"
      ? [{ role: "standard", card: template.card }]
      : template.template_kind === "tiered"
        ? [
            { role: "tier_base", card: template.base_card },
            { role: "tier_above", card: template.tier?.card },
          ]
        : [
            { role: "peak", card: template.peak_card },
            { role: "offpeak", card: template.offpeak_card },
          ];
  return (
    <OperatorInsetPanel>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-foreground">
          {copy.multiCardSummary(cards.length)}
        </p>
        {template.template_kind === "tiered" && template.tier ? (
          <p className="text-xs text-muted-foreground">
            {copy.tierDetailsDescription(template.tier.input_tokens_above)}
          </p>
        ) : null}
        {template.template_kind === "peak_valley" && template.schedule ? (
          <p className="text-xs text-muted-foreground">
            {template.schedule.timezone} ·{" "}
            {templateDialogCopy.scheduleWindowsSummary(
              template.schedule.windows.length,
            )}
          </p>
        ) : null}
      </div>
      <div className="mt-3 flex flex-col gap-4">
        {cards.map(({ role, card }) => (
          <div key={role} className="flex flex-col gap-2">
            <OperatorValueBadge
              label={cardRoleLabel(role, copy)}
              className="w-fit text-xs"
            />
            {card ? (
              <CardRateGrid
                card={card}
                symbol={template.active_currency_symbol}
              />
            ) : (
              <OperatorMissingValue reason={copy.referencesUnavailableReason} />
            )}
          </div>
        ))}
      </div>
    </OperatorInsetPanel>
  );
}

/**
 * Revision history shows all five rates, not just the two base ones, and marks
 * which of them actually changed between consecutive versions.
 */
function HistoryPanel({
  loading,
  revisions,
}: {
  loading: boolean;
  revisions: PricingTemplateRevision[];
}) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.pricingTemplatesUi;
  const historyCopy = messages.pricingTemplatesHistory;

  if (loading) return <Skeleton className="h-20 rounded-md" />;
  if (revisions.length === 0) {
    return (
      <OperatorInsetPanel>
        <p className="text-xs text-muted-foreground">{historyCopy.empty}</p>
      </OperatorInsetPanel>
    );
  }

  return (
    <OperatorInsetPanel className="p-0">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.columnVersion}</TableHead>
              <TableHead>{historyCopy.tableKind}</TableHead>
              <TableHead>{historyCopy.tableCards}</TableHead>
              <TableHead>{historyCopy.effectiveAt}</TableHead>
              <TableHead>{historyCopy.createdBy}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((revision, index) => {
              const previous = revisions[index + 1];
              const structure = JSON.stringify({
                kind: revision.template_kind,
                card: revision.card,
                base: revision.base_card,
                tier: revision.tier,
                peak: revision.peak_card,
                offpeak: revision.offpeak_card,
                schedule: revision.schedule,
              });
              const previousStructure = previous
                ? JSON.stringify({
                    kind: previous.template_kind,
                    card: previous.card,
                    base: previous.base_card,
                    tier: previous.tier,
                    peak: previous.peak_card,
                    offpeak: previous.offpeak_card,
                    schedule: previous.schedule,
                  })
                : structure;
              const changed = Boolean(
                previous && structure !== previousStructure,
              );
              const cardCount = [
                revision.card,
                revision.base_card,
                revision.tier?.card,
                revision.peak_card,
                revision.offpeak_card,
              ].filter(Boolean).length;
              return (
                <TableRow key={revision.revision_id}>
                  <TableCell>
                    <OperatorValueBadge
                      label={`v${revision.version}`}
                      className="text-xs"
                    />
                  </TableCell>
                  <TableCell>
                    <OperatorValueBadge
                      label={
                        revision.template_kind === "standard"
                          ? copy.kindStandard
                          : revision.template_kind === "tiered"
                            ? copy.kindTiered
                            : copy.kindPeakValley
                      }
                      className="text-xs"
                    />
                  </TableCell>
                  <TableCell className="text-xs">
                    <span>{copy.multiCardSummary(cardCount)}</span>
                    {changed ? (
                      <span className="ml-2 text-failing">
                        {historyCopy.structureChanged}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">
                    {revision.effective_at ? (
                      formatTime(revision.effective_at)
                    ) : (
                      <OperatorMissingValue className="text-xs" />
                    )}
                  </TableCell>
                  <TableCell className="text-xs">
                    {historyCopy.createdByKind(revision.created_by_kind)}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </OperatorInsetPanel>
  );
}

export { HistoryPanel, RateCell, TierPanel, UsagePanel };
