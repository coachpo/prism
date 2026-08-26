import type { ManagedModelConfigListItem } from "@/lib/api/models"

/**
 * A `single` strategy uses the first enabled target and nothing else, so any
 * further enabled targets on that model are configured but unreachable. The
 * list surfaces this because the model otherwise looks correctly configured.
 */
export function isSingleTruncated(model: ManagedModelConfigListItem) {
  if (model.loadbalance_strategy?.legacy_strategy_type !== "single") return false
  return model.access_targets.filter((target) => target.is_enabled).length >= 2
}
