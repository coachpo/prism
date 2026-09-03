import type { ManagedModelConfigListItem } from "@/lib/api/models"
import { getTerminalTarget, isTerminalTargetAccessTargetType } from "@/lib/types/target-compatibility"

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

/**
 * At least one DIRECT Terminal Target holds a persisted upstream identity that
 * differs from the owning configuration's `model_id`. The comparison is exact and
 * case-sensitive: identities are provider-facing strings, and `Entry-A` and
 * `entry-a` are different upstream identities. A Terminal Target with no
 * readable identity is unknown evidence, not a decoupled one — claiming
 * decoupling without the persisted value would fabricate a fact.
 */
export function isUpstreamDecoupled(model: ManagedModelConfigListItem) {
  const ownerModelId = model.model_id
  return model.access_targets.some((target) => {
    if (!isTerminalTargetAccessTargetType(target.target_type)) return false
    const upstreamModelId = getTerminalTarget(target)?.upstream_model_id
    if (!upstreamModelId?.trim()) return false
    return upstreamModelId !== ownerModelId
  })
}
