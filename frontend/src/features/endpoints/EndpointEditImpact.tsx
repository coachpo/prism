import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout } from "@/shared/design-system"
import type { EndpointReferenceDetailState } from "./useEndpointReferenceDetails"

export function EndpointEditImpact({ state, onRetry, onLoadMore }: { state?: EndpointReferenceDetailState; onRetry: () => void; onLoadMore: () => void }) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  if (!state || state.status === "idle" || state.status === "loading" && !state.previous) return <p role="status" className="text-xs text-muted-foreground">{copy.deleteChecking}</p>
  if (state.status === "error") return <OperatorCallout intent="warning" description={copy.editImpactUnavailable} action={<Button type="button" size="sm" variant="outline" onClick={onRetry}>{copy.deleteRetry}</Button>} />
  const snapshot = state.status === "loading" ? state.previous : state.value
  if (!snapshot) return null
  const names = [...new Map(snapshot.loaded_items.flatMap((item) => item.owner_model ? [[item.owner_model.id, item.owner_model.display_name || item.owner_model.model_id] as const] : [])).values()]
  return <OperatorCallout intent={state.status === "stale" ? "warning" : "info"} title={copy.editImpactModels(snapshot.summary.referencing_model_count)} description={<div className="flex flex-col gap-1"><p>{names.length ? names.join("、") : snapshot.summary.referencing_model_count > 0 ? copy.editImpactMoreModels : copy.editImpactNoModels}</p>{snapshot.summary.orphan_reference_count > 0 ? <p>{copy.editImpactOrphans}</p> : null}{state.status === "stale" ? <p>{copy.editImpactUnavailable}</p> : null}</div>} action={snapshot.next_cursor ? <Button type="button" size="sm" variant="outline" disabled={state.status === "loading"} onClick={onLoadMore}>{copy.loadMore}</Button> : state.status === "stale" ? <Button type="button" size="sm" variant="outline" onClick={onRetry}>{copy.deleteRetry}</Button> : undefined} />
}
