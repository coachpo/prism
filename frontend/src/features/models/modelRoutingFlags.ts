import type { ManagedModelConfigListItem } from "@/lib/api/models"

/**
 * A `single` strategy uses the first enabled target and nothing else, so any
 * further enabled targets on that model are configured but unreachable. The
 * list surfaces this because the model otherwise looks correctly configured.
 */
export function isSingleTruncated(model: ManagedModelConfigListItem) {
  return (model.routing_summary?.single_truncated_access_target_ids.length ?? 0) > 0
}
