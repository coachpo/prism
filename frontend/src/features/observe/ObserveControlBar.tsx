import { OBSERVE_PRESETS, type ObservePreset } from "@/features/observe/observeSearch";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";

/**
 * Window selection only. Refreshing belongs to the freshness bar, which is
 * also where the page states when the data on screen is from.
 */
export function ObserveControlBar({
  preset,
  onPresetChange,
}: {
  preset: ObservePreset;
  onPresetChange: (preset: ObservePreset) => void;
}) {
  const { messages } = useLocale();

  return (
    <div
      className="flex w-fit items-center gap-0.5 rounded-md border border-border bg-inset p-0.5"
      role="group"
      aria-label={messages.observe.timeRangeLabel}
    >
      {OBSERVE_PRESETS.map((value) => (
        <button
          key={value}
          type="button"
          onClick={() => onPresetChange(value)}
          aria-pressed={preset === value}
          // 这排按钮给整页 KPI 与四个视图重新定基，命中区不能低于 28×28；
          // 标签是中文，等宽只留给数字（混排等宽会撕裂字形）。
          className={cn(
            "inline-flex min-h-7 items-center rounded-[4px] px-2.5 text-xs font-medium tabular-nums transition-colors",
            preset === value
              ? "bg-primary-soft text-on-primary-soft"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {messages.observe.presetName(value)}
        </button>
      ))}
    </div>
  );
}
