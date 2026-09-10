import type { ChainRankingCoverage } from "@/lib/types";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { CHAIN_SORT_BY_OPTIONS } from "./queryParams";
import type { RequestLogPageActions } from "./useRequestLogPageState";

export function ChainRankingControls({ actions, ranking, pending = false }: {
  actions: Pick<RequestLogPageActions, "state" | "setSort">;
  ranking?: ChainRankingCoverage | null;
  pending?: boolean;
}) {
  const { messages, formatNumber } = useLocale();
  const copy = messages.chainRanking;
  const { state } = actions;
  if (state.view !== "ingress_chains") return null;
  const isTime = state.sort_by === "created_at";
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground">{copy.title}</span>
        <ToggleGroup type="single" variant="outline" size="sm" value={state.sort_by} aria-label={copy.title}
          onValueChange={(value) => { if (value) actions.setSort(value, "desc"); }}>
          {CHAIN_SORT_BY_OPTIONS.map((metric) => <ToggleGroupItem key={metric} value={metric}>{copy[metric]}</ToggleGroupItem>)}
        </ToggleGroup>
        <Button size="sm" variant="ghost" aria-label={copy.direction}
          onClick={() => actions.setSort(state.sort_by, state.sort_order === "desc" ? "asc" : "desc")}>
          {isTime ? (state.sort_order === "desc" ? copy.timeDesc : copy.timeAsc) : copy[state.sort_order]}
        </Button>
      </div>
      {!isTime ? <p className="text-xs text-muted-foreground">{state.sort_by === "total_cost_user_currency_micros" ? copy.costScope : copy.scope}</p> : null}
      {!isTime ? ranking ? <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs" aria-live="polite">
        <span>{copy.counts(formatNumber(ranking.rankable_ingress_count), formatNumber(ranking.unrankable_ingress_count))}</span>
        {Object.entries(ranking.unrankable_reasons).filter(([, count]) => count > 0).map(([reason, count]) =>
          <span className="text-muted-foreground" key={reason}>{copy.countReasons[reason as keyof typeof copy.countReasons] ?? copy.unknown}：{formatNumber(count)}</span>)}
      </div> : <p className="text-xs text-muted-foreground">{pending ? copy.countsLoading : copy.countsUnavailable}</p> : null}
    </div>
  );
}
