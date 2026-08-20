import type { PricingCard, PricingTemplate } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorStatusBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import { normalizeTemplatePrice } from "./pricingSchemas";

export function RateCell({
  symbol,
  value,
  specialty,
}: {
  symbol?: string;
  value: string | null | undefined;
  specialty?: boolean;
}) {
  const copy = useLocale().messages.pricingTemplatesUi;
  const normalized = normalizeTemplatePrice(value);
  if (normalized === "") {
    if (!specialty) return <OperatorMissingValue className="text-xs" />;
    return (
      <span className="inline-flex items-center justify-end gap-1">
        <OperatorMissingValue className="text-xs" reason={copy.rateUnconfiguredReason} />
        <OperatorStatusBadge intent="idle" preserveLabel label={copy.rateUnconfigured} />
      </span>
    );
  }
  return (
    <span className="font-mono text-xs tabular-nums">
      {symbol ? <span className={cn("text-muted-foreground", symbol.length > 1 && "mr-1")}>{symbol}</span> : null}
      {normalized}
    </span>
  );
}

function cardRoleLabel(role: string, copy: ReturnType<typeof useLocale>["messages"]["pricingTemplatesUi"]) {
  switch (role) {
    case "standard": return copy.cardStandard;
    case "tier_base": return copy.cardTierBase;
    case "tier_above": return copy.cardTierAbove;
    case "peak": return copy.cardPeak;
    case "offpeak": return copy.cardOffpeak;
    default: return copy.multiCardSummary(1);
  }
}

function CardRateGrid({ card, symbol }: { card: PricingCard; symbol: string }) {
  const copy = useLocale().messages.pricingTemplatesUi;
  const fields: Array<[keyof PricingCard, string, boolean]> = [
    ["input_price", copy.rateInput, false], ["output_price", copy.rateOutput, false],
    ["cached_input_price", copy.rateCachedInput, true], ["cache_creation_price", copy.rateCacheCreation, true],
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

export function PricingTemplateRatePanel({ template }: { template: PricingTemplate }) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const cards: Array<{ role: string; card: PricingCard | null | undefined }> =
    template.template_kind === "standard"
      ? [{ role: "standard", card: template.card }]
      : template.template_kind === "tiered"
        ? [{ role: "tier_base", card: template.base_card }, { role: "tier_above", card: template.tier.card }]
        : [{ role: "peak", card: template.peak_card }, { role: "offpeak", card: template.offpeak_card }];
  return (
    <OperatorInsetPanel>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-foreground">{copy.multiCardSummary(cards.length)}</p>
        {template.template_kind === "tiered" ? <p className="text-xs text-muted-foreground">{copy.tierDetailsDescription(template.tier.input_tokens_above)}</p> : null}
        {template.template_kind === "peak_valley" ? <p className="text-xs text-muted-foreground">{template.schedule.timezone} · {messages.pricingTemplateDialog.scheduleWindowsSummary(template.schedule.windows.length)}</p> : null}
      </div>
      <div className="mt-3 flex flex-col gap-4">
        {cards.map(({ role, card }) => (
          <div key={role} className="flex flex-col gap-2">
            <OperatorValueBadge label={cardRoleLabel(role, copy)} className="w-fit text-xs" />
            {card ? <CardRateGrid card={card} symbol={template.active_currency_symbol} /> : <OperatorMissingValue reason={copy.referencesUnavailableReason} />}
          </div>
        ))}
      </div>
    </OperatorInsetPanel>
  );
}
