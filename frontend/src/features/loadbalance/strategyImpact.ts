import type { StrategyImpactListResponse } from "@/lib/types";

/**
 * Pure helpers for the Strategy Impact (attached models) list. Kept out of the
 * feature hook so the stable-ID contract is unit-testable without React.
 */

/**
 * Append one page of attached models to the accumulated list, deduplicating by
 * `model_config_id`. Cursor pages can overlap when rows shift between reads;
 * a model must never appear twice just because two page boundaries disagreed.
 */
export function mergeStrategyImpactItems(
	existing: StrategyImpactListResponse["items"] | undefined,
	incoming: StrategyImpactListResponse["items"],
): StrategyImpactListResponse["items"] {
	if (!existing || existing.length === 0) return [...incoming];
	const seen = new Set(existing.map((item) => item.model_config_id));
	return [
		...existing,
		...incoming.filter((item) => {
			if (seen.has(item.model_config_id)) return false;
			seen.add(item.model_config_id);
			return true;
		}),
	];
}
