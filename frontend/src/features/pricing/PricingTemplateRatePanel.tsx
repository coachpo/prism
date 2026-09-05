import type { PricingCard, PricingTemplate, PricingTemplateWindow } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorStatusBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import { cardRoleLabel, templateRateCards } from "./pricingRateCards";
import { normalizeTemplatePrice } from "./pricingSchemas";

export function RateCell({ symbol, value, specialty }: { symbol?: string; value: string | null | undefined; specialty?: boolean }) {
  const copy = useLocale().messages.pricingTemplatesUi;
  const normalized = normalizeTemplatePrice(value);
  if (normalized === "") {
    if (!specialty) return <OperatorMissingValue className="text-xs" />;
    return <span className="inline-flex items-center justify-end gap-1"><OperatorMissingValue className="text-xs" reason={copy.rateUnconfiguredReason} /><OperatorStatusBadge intent="idle" preserveLabel label={copy.rateUnconfigured} /></span>;
  }
  return <span className="font-mono text-xs tabular-nums">{symbol ? <span className={cn("text-muted-foreground", symbol.length > 1 && "mr-1")}>{symbol}</span> : null}{normalized}</span>;
}

function CardRateGrid({ card, symbol }: { card: PricingCard; symbol: string }) {
  const copy = useLocale().messages.pricingTemplatesUi;
  const fields: Array<[keyof PricingCard, string, boolean]> = [["input_price", copy.rateInput, false], ["output_price", copy.rateOutput, false], ["cached_input_price", copy.rateCachedInput, true], ["cache_creation_price", copy.rateCacheCreation, true], ["reasoning_price", copy.rateReasoning, true]];
  return <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">{fields.map(([field, label, specialty]) => <div key={field}><p className="text-xs text-muted-foreground">{label}</p><RateCell specialty={specialty} symbol={symbol} value={card[field]} /></div>)}</div>;
}

function formatMinute(value: number) {
  const minute = ((value % 1440) + 1440) % 1440;
  return `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
}

function formatWindow(window: PricingTemplateWindow, weekdayLabels: string[]) {
  const days = weekdayLabels.filter((_, bit) => (window.weekday_mask & (1 << bit)) !== 0).join("、");
  return `${days || "—"} ${formatMinute(window.start_minute)}–${formatMinute(window.end_minute)}`;
}

export function PricingTemplateRatePanel({ template }: { template: PricingTemplate }) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const kind = (template as { template_kind?: string }).template_kind;
  const scheduleWindows = Array.isArray(template.schedule?.windows) ? template.schedule.windows : [];
  const cards = templateRateCards(template);
  if (cards.length === 0) return <OperatorErrorState title={messages.pricingTemplatesData.loadFailed} description={messages.pricingTemplatesHistory.unknownKind} />;
  if (kind === "peak_valley" && (!template.schedule || scheduleWindows.length === 0 || !template.schedule.timezone)) {
    return <OperatorErrorState title={messages.pricingTemplatesData.loadFailed} description={messages.pricingTemplatesHistory.scheduleUnavailable} />;
  }
  return (
    <OperatorInsetPanel>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-foreground">{copy.multiCardSummary(cards.length)}</p>
        {kind === "tiered" && template.tier ? <p className="text-xs text-muted-foreground">{copy.tierDetailsDescription(template.tier.input_tokens_above)}</p> : null}
        {kind === "peak_valley" && template.schedule ? <div className="text-xs text-muted-foreground"><p>{template.schedule.timezone} · {messages.pricingTemplateDialog.scheduleWindowsSummary(scheduleWindows.length)}</p><ul className="mt-1 list-inside list-disc">{scheduleWindows.map((window, index) => <li key={`${index}-${window.weekday_mask}-${window.start_minute}`}>{formatWindow(window, messages.pricingTemplateDialog.weekdayLabels)}</li>)}</ul></div> : null}
      </div>
      <div className="mt-3 flex flex-col gap-4">
        {cards.map(({ role, card }) => <div key={role} className="flex flex-col gap-2"><OperatorValueBadge label={cardRoleLabel(role, copy)} className="w-fit text-xs" />{card ? <CardRateGrid card={card} symbol={template.active_currency_symbol} /> : <OperatorMissingValue reason={messages.honesty.noValue} />}</div>)}
      </div>
    </OperatorInsetPanel>
  );
}
