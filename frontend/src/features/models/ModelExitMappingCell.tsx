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

  const [firstItem, ...restItems] = visible

  // 计数与第一条映射同行、其余与「还有 N 项」同行：这个单元格原本能长到
  // 四行 95px，把整张表的行高拉到契约值的两倍多。
  return (
    <span className="flex min-w-48 flex-col gap-0.5">
      <span className="flex min-w-0 flex-wrap items-baseline gap-x-1.5">
        <span className="font-mono text-xs tabular-nums">
          {copy.targetsCount(
            formatNumber(enabledTargets ?? 0),
            formatNumber(totalTargets),
          )}
        </span>
        {firstItem ? (
          <ExitMappingItemLine
            directRequestEnabled={model.direct_request_enabled}
            item={firstItem}
            ownerModelId={model.model_id}
          />
        ) : null}
      </span>
      {restItems.length > 0 || remainingCount > 0 ? (
        <span className="flex min-w-0 flex-wrap items-baseline gap-x-1.5">
          {restItems.map((item) => (
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
        <span className="shrink-0 text-muted-foreground">
          {copy.exitModelTargetPrefix}
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
        entrySame ? (
          <span
            className="shrink-0 text-xs text-muted-foreground"
            title={copy.exitEntrySameReason(ownerModelId)}
          >
            {copy.exitEntrySame}
          </span>
        ) : (
          <OperatorTypeBadge
            intent="accent"
            label={copy.exitUpstreamOnly}
            preserveLabel
            title={
              directRequestEnabled
                ? copy.exitUpstreamOnlyReason(ownerModelId, upstreamModelId)
                : copy.exitUpstreamOnlyNonEntryReason(ownerModelId, upstreamModelId)
            }
          />
        )
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
