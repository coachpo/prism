import type { ChainIngressItem } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";
import { OperatorMissingValue } from "@/shared/design-system";
import { formatDurationMs } from "./requestLogMetricPresentation";

export function ChainRankingValue({ ranking }: { ranking: ChainIngressItem["ranking"] }) {
  const { messages, formatNumber } = useLocale();
  const copy = messages.chainRanking;
  if (!ranking || ranking.state !== "ranked" || ranking.value === null) {
    const reason = ranking && ranking.state !== "ranked" ? copy[ranking.state] : copy.unknown;
    return <div className="flex flex-col gap-0.5"><OperatorMissingValue reason={reason} /><span className="text-xs text-muted-foreground">{reason}</span></div>;
  }
  if (ranking.metric === "elapsed_ms") return <span>{formatDurationMs(ranking.value)}</span>;
  const [segment, currency] = ranking.group.split(":");
  return <div className="flex flex-col gap-0.5">
    <span>{currency} {formatNumber(ranking.value / 1_000_000, { maximumFractionDigits: 6 })}</span>
    <span className="text-xs text-muted-foreground">{segment.startsWith("e.") ? copy.epoch(segment.slice(2)) : copy.legacy}</span>
  </div>;
}
