import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useLocale } from "@/i18n/useLocale"
import type { ModelInventoryView } from "./modelView"

type ModelInventoryViewSwitchProps = {
  onViewChange: (view: ModelInventoryView) => void
  view: ModelInventoryView
}

export function ModelInventoryViewSwitch({
  onViewChange,
  view,
}: ModelInventoryViewSwitchProps) {
  const { messages } = useLocale()
  const copy = messages.modelsPage

  return (
    <div
      className="flex flex-col gap-1"
      data-testid="models-inventory-view-switcher"
    >
      <span
        id="models-inventory-view-label"
        className="text-xs font-medium text-foreground"
      >
        {copy.viewLabel}
      </span>
      <ToggleGroup
        aria-labelledby="models-inventory-view-label"
        className="max-w-full flex-wrap justify-start"
        onValueChange={(value) => {
          if (
            value === "entries" ||
            value === "model_targets" ||
            value === "all"
          ) {
            onViewChange(value)
          }
        }}
        size="sm"
        type="single"
        value={view}
        variant="outline"
      >
        {/* URL 驱动的分段控件里，方向键要「选中」而不只是「预览」：
            同一页的 Tabs 按方向键就直接改 URL，两套语义会互相矛盾。 */}
        <ToggleGroupItem value="entries" onFocus={() => onViewChange("entries")}>
          {copy.viewEntries}
        </ToggleGroupItem>
        <ToggleGroupItem
          value="model_targets"
          onFocus={() => onViewChange("model_targets")}
        >
          {copy.viewModelTargets}
        </ToggleGroupItem>
        <ToggleGroupItem value="all" onFocus={() => onViewChange("all")}>
          {copy.viewAll}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
  )
}
