import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { stampRepeatsPage, useObservePageGeneratedAt } from "./observeStampReference";

/**
 * Stamps stay attached to the response they describe, including last-good
 * fragments. Under a freshness bar a stamp that repeats the bar's time is left
 * out; a stale fragment keeps its own, because its staleness badge carries no
 * time of its own.
 */
export function ObserveFragmentStamp({ generatedAt, from, to, stale = false }: { generatedAt?: string; from?: string; to?: string; stale?: boolean }) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const pageGeneratedAt = useObservePageGeneratedAt();
  if (!generatedAt) return null;
  if (!stale && stampRepeatsPage(generatedAt, pageGeneratedAt)) return null;
  return <p className="text-xs text-muted-foreground">
    {messages.observe.fragmentSampled}：<span className="font-mono tabular-nums">{format(generatedAt)}</span>
    {from && to && <> · {messages.observe.fragmentRange}：<span className="font-mono tabular-nums">{format(from)} — {format(to)}</span></>}
  </p>;
}
