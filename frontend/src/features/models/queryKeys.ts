import type { RewriteProfileId } from "@/shared/api/queryKeys"

export type ModelsListFilters = {
  search: string
  api_family: string
  vendor_id: string
  status: string
}

export const DEFAULT_MODELS_LIST_FILTERS: ModelsListFilters = {
  search: "",
  api_family: "all",
  vendor_id: "all",
  status: "all",
}

export function normalizeModelsListFilters(filters: Partial<ModelsListFilters> = {}): ModelsListFilters {
  return {
    search: filters.search?.trim() ?? "",
    api_family: filters.api_family?.trim() || "all",
    vendor_id: filters.vendor_id?.trim() || "all",
    status: filters.status?.trim() || "all",
  }
}

export const modelsQueryKeys = {
  all: (profileId: RewriteProfileId | null | undefined) => ["rewrite", "selected-profile", String(profileId ?? "none"), "models"] as const,
  list: (profileId: RewriteProfileId | null | undefined, filters: Partial<ModelsListFilters> = {}) =>
    [...modelsQueryKeys.all(profileId), "list", normalizeModelsListFilters(filters)] as const,
}
