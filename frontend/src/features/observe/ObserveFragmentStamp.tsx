import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";

/** Stamps stay attached to the response they describe, including last-good fragments. */
export function ObserveFragmentStamp({ generatedAt, from, to }: { generatedAt?: string; from?: string; to?: string }) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  if (!generatedAt) return null;
  return <p className="text-xs text-muted-foreground">
    {messages.observe.fragmentSampled}：<span className="font-mono tabular-nums">{format(generatedAt)}</span>
    {from && to && <> · {messages.observe.fragmentRange}：<span className="font-mono tabular-nums">{format(from)} — {format(to)}</span></>}
  </p>;
}
