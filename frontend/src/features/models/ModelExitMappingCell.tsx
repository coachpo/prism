import { useLocale } from "@/i18n/useLocale"
import type { ManagedModelConfigListItem } from "@/lib/api/models"
import {
  OperatorMissingValue,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system"
import { projectExitMapping } from "./modelExitMapping"

/**
 * Content of the models-list exit-mapping cell. ModelsTable wraps this in the
 * detail Link, so everything rendered here — including the remainder line —
 * navigates to the model-config detail.
 *
 * Honesty rules from frontend/DESIGN.md apply per value: a missing endpoint or
 * upstream identity renders `—` with a reason and is never backfilled with the
 * owning `model_id`; identity and non-participation are textual states, never
 * color-only.
 */
export function ModelExitMappingCell({ model }: { model: ManagedModelConfigListItem }) {
  const { formatNumber, messages } = useLocale()
  const copy = messages.modelsPage
  const enabledTargets = model.routing_summary?.enabled_access_target_count
  const totalTargets = model.routing_summary?.total_access_target_count

  if (totalTargets == null) {
    return <OperatorMissingValue reason={copy.exitSummaryUnavailable} />
  }
  if (totalTargets === 0) {
    return (
      <OperatorStatusBadge
        intent="failing"
        preserveLabel
        label={copy.targetsNone}
        title={copy.targetsNoneReason}
      />
    )
  }

  const { visible, remainingCount } = projectExitMapping(model)

  return (
    <span className="flex min-w-56 flex-col gap-0.5">
      <span className="font-mono text-xs tabular-nums">
        {copy.targetsCount(
          formatNumber(enabledTargets ?? 0),
          formatNumber(totalTargets),
        )}
      </span>
      {visible.map((item) => (
        <ExitMappingItemLine
          directRequestEnabled={model.direct_request_enabled}
          item={item}
          key={item.accessTargetId}
          ownerModelId={model.model_id}
        />
      ))}
      {remainingCount > 0 ? (
        <span className="text-xs text-muted-foreground">
          {copy.exitRemainder(formatNumber(remainingCount))}
        </span>
      ) : null}
    </span>
  )
}

function ExitMappingItemLine({
  directRequestEnabled,
  item,
  ownerModelId,
}: {
  directRequestEnabled: boolean
  item: ReturnType<typeof projectExitMapping>["visible"][number]
  ownerModelId: string
}) {
  const { messages } = useLocale()
  const copy = messages.modelsPage

  if (item.identity.kind === "model") {
    const logicalModelId = item.identity.logicalModelId
    return (
      <span className="flex min-w-0 items-center gap-1 text-xs">
        <span aria-hidden="true" className="shrink-0 text-muted-foreground">
          →
        </span>
        {logicalModelId ? (
          <span
            className="truncate font-mono text-muted-foreground"
            title={copy.exitModelTargetReason(logicalModelId)}
          >
            {logicalModelId}
          </span>
        ) : (
          <OperatorMissingValue
            className="text-xs"
            reason={copy.exitModelTargetMissing}
          />
        )}
        {!item.isEnabled ? (
          <span
            className="shrink-0 text-muted-foreground"
            title={copy.exitNotParticipatingReason}
          >
            {copy.exitNotParticipating}
          </span>
        ) : null}
      </span>
    )
  }

  const { endpointName, upstreamModelId } = item.identity
  const entrySame = directRequestEnabled && upstreamModelId === ownerModelId
  return (
    <span className="flex min-w-0 items-center gap-1 text-xs">
      {endpointName ? (
        <span
          className="max-w-32 truncate text-muted-foreground"
          title={endpointName}
        >
          {endpointName}
        </span>
      ) : (
        <OperatorMissingValue
          className="text-xs"
          reason={copy.exitEndpointMissingReason}
        />
      )}
      <span aria-hidden="true" className="shrink-0 text-muted-foreground">
        →
      </span>
      {upstreamModelId ? (
        <span className="truncate font-mono" title={upstreamModelId}>
          {upstreamModelId}
        </span>
      ) : (
        <OperatorMissingValue
          className="text-xs"
          reason={copy.exitUpstreamMissingReason}
        />
      )}
      {upstreamModelId ? (
        <OperatorTypeBadge
          intent={entrySame ? "neutral" : "accent"}
          label={entrySame ? copy.exitEntrySame : copy.exitUpstreamOnly}
          preserveLabel
          title={
            entrySame
              ? copy.exitEntrySameReason(ownerModelId)
              : directRequestEnabled
                ? copy.exitUpstreamOnlyReason(ownerModelId, upstreamModelId)
                : copy.exitUpstreamOnlyNonEntryReason(ownerModelId, upstreamModelId)
          }
        />
      ) : null}
      {!item.isEnabled ? (
        <span
          className="shrink-0 text-muted-foreground"
          title={copy.exitNotParticipatingReason}
        >
          {copy.exitNotParticipating}
        </span>
      ) : null}
    </span>
  )
}
