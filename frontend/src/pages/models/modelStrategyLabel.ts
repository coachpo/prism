import { getStaticMessages } from "@/i18n/staticMessages";
import { getLoadbalanceStrategyTypeLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategySummary } from "@/lib/types";

export function modelStrategyLabel(strategy: Pick<LoadbalanceStrategySummary, "name" | "legacy_strategy_type">): string {
  const copy = getStaticMessages();
  if (strategy.name === `Default ${strategy.legacy_strategy_type} routing`) {
    return copy.modelsUi.defaultStrategyLabel(getLoadbalanceStrategyTypeLabel(strategy, copy.loadbalanceStrategyCopy));
  }
  return strategy.name;
}
