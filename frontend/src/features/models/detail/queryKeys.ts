import type { ModelDetailTab } from "./modelDetailSchemas"

export const modelDetailQueryKeys = {
  detail: (selectedProfileId: number | null, modelConfigId: string | number) => [
    "rewrite",
    "selected-profile",
    selectedProfileId == null ? "none" : String(selectedProfileId),
    "models",
    "detail",
    String(modelConfigId),
  ] as const,
  tab: (selectedProfileId: number | null, modelConfigId: string | number, tab: ModelDetailTab) => [
    ...modelDetailQueryKeys.detail(selectedProfileId, modelConfigId),
    "tab",
    tab,
  ] as const,
}
