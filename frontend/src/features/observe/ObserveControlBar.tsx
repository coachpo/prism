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
          className={cn(
            "rounded-[4px] px-2.5 py-1 font-mono text-xs font-medium tabular-nums transition-colors",
            preset === value
              ? "bg-primary-soft text-on-primary-soft"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {value}
        </button>
      ))}
    </div>
  );
}
