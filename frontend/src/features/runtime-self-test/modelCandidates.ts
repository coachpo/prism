import type { ModelConfigListItem } from "@/lib/types"

export function runtimeSelfTestModelCandidates(
  models: ModelConfigListItem[],
): ModelConfigListItem[] {
  return models.filter(
    (model) => model.is_enabled && model.direct_request_enabled === true,
  )
}
