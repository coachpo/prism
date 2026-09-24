import type { ManagedModelConfigListItem } from "@/lib/api/models"

export function isDirectRequestEntry(model: Pick<ManagedModelConfigListItem, "direct_request_enabled">) {
  return model.direct_request_enabled === true
}

/**
 * A `single` strategy uses the first enabled target and nothing else, so any
 * further enabled targets on that model are configured but unreachable. The
 * list surfaces this because the model otherwise looks correctly configured.
 */
export function isSingleTruncated(model: ManagedModelConfigListItem) {
  return (model.routing_summary?.single_truncated_access_target_ids.length ?? 0) > 0
}

/**
 * The model configuration carries at least one Model Target row — a logical edge that
 * resolves further before reaching a Terminal Target. This is a structural
 * fact about the direct access-target list, not about traffic.
 */
export function hasModelTarget(model: ManagedModelConfigListItem) {
  return model.access_targets.some((target) => target.target_type === "model")
}
