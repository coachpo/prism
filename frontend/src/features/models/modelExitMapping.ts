import type { ManagedModelConfigListItem } from "@/lib/api/models"
import type { ModelAccessTarget } from "@/lib/types"
import { getTerminalTarget } from "@/lib/types/target-compatibility"

/**
 * The models list exit-mapping projection.
 *
 * The list cell answers "where does this model configuration exit" from the DIRECT
 * `access_targets` rows only: Terminal Target rows carry the actual provider
 * exit (endpoint + persisted `upstream_model_id`), Model Target rows are
 * logical edges that resolve further. The projection never follows Model
 * Target rows and never infers an identity from the owning `model_id`.
 *
 * Ordering is the shared runtime order `(position, id)`; the backend already
 * returns rows sorted this way, but the projection sorts defensively because
 * the cell's first-two/remainder split is only honest when the order is the
 * order routing actually uses.
 */
export const EXIT_MAPPING_VISIBLE_ITEMS = 2

export type ExitMappingIdentity =
  | {
      kind: "terminal"
      /** Endpoint display name; null when the row carries no endpoint reference. */
      endpointName: string | null
      /** Persisted upstream identity; null when the row carries no evidence. */
      upstreamModelId: string | null
    }
  | {
      kind: "model"
      /** Logical target identity; null when neither summary nor raw id exists. */
      logicalModelId: string | null
    }

export interface ExitMappingItem {
  accessTargetId: number
  position: number
  isEnabled: boolean
  identity: ExitMappingIdentity
}

export interface ExitMappingProjection {
  /** First `EXIT_MAPPING_VISIBLE_ITEMS` rows in `(position, id)` order. */
  visible: ExitMappingItem[]
  /** Direct rows beyond the visible window; the cell links them to detail. */
  remainingCount: number
}

function identityOf(target: ModelAccessTarget): ExitMappingIdentity {
  if (target.target_type === "model") {
    return { kind: "model", logicalModelId: target.target_model?.model_id ?? target.target_model_id ?? null }
  }
  const terminal = getTerminalTarget(target)
  return {
    kind: "terminal",
    endpointName: terminal?.endpoint?.name ?? null,
    upstreamModelId: terminal?.upstream_model_id?.trim() ? terminal.upstream_model_id : null,
  }
}

export function projectExitMapping(
  model: Pick<ManagedModelConfigListItem, "access_targets">,
): ExitMappingProjection {
  const seen = new Set<number>()
  const ordered = [...model.access_targets]
    .sort((left, right) => left.position - right.position || left.id - right.id)
    .filter((target) => {
      // The projection addresses rows by their persisted id; a duplicated id
      // would show the same physical target twice.
      if (seen.has(target.id)) return false
      seen.add(target.id)
      return true
    })
    .map<ExitMappingItem>((target) => ({
      accessTargetId: target.id,
      position: target.position,
      isEnabled: target.is_enabled,
      identity: identityOf(target),
    }))
  return {
    visible: ordered.slice(0, EXIT_MAPPING_VISIBLE_ITEMS),
    remainingCount: Math.max(0, ordered.length - EXIT_MAPPING_VISIBLE_ITEMS),
  }
}
