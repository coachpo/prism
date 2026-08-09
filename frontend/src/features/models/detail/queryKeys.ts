export const modelDetailQueryKeys = {
  detail: (selectedProfileId: number | null, modelConfigId: string | number) => [
    "rewrite",
    "selected-profile",
    selectedProfileId == null ? "none" : String(selectedProfileId),
    "models",
    "detail",
    String(modelConfigId),
  ] as const,
}
