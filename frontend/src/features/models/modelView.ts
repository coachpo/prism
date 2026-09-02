import type { ManagedModelConfigListItem } from "@/lib/api/models"
import { isDirectRequestEntry } from "./modelRoutingFlags"

export type ModelInventoryView = "entries" | "model_targets" | "all"

export function filterModelsByInventoryView(
  models: ManagedModelConfigListItem[],
  view: ModelInventoryView,
): ManagedModelConfigListItem[] {
  if (view === "all") return models
  if (view === "model_targets") {
    return models.filter((model) => !isDirectRequestEntry(model))
  }
  return models.filter(isDirectRequestEntry)
}
