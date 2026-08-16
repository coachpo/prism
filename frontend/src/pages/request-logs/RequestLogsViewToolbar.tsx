import type { ReactNode } from "react";

import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type { RequestLogView } from "./queryParams";

/**
 * The view switcher and the controls both views share.
 *
 * Column display, page size, and export used to hang off the attempts table
 * only, so the landing view had no way to reach them. They live here instead,
 * above whichever table is showing.
 */
export function RequestLogsViewToolbar({
  children,
  onViewChange,
  summary,
  view,
}: {
  children?: ReactNode;
  onViewChange: (view: RequestLogView) => void;
  summary?: ReactNode;
  view: RequestLogView;
}) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;

  const options: { label: string; value: RequestLogView }[] = [
    { label: copy.viewIngressChains, value: "ingress_chains" },
    { label: copy.viewAttempts, value: "attempts" },
  ];

  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <div
          role="group"
          aria-label={copy.viewSwitcherLabel}
          className="flex w-fit items-center gap-0.5 rounded-md border border-border bg-inset p-0.5"
        >
          {options.map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => onViewChange(option.value)}
              aria-pressed={view === option.value}
              data-testid={`request-logs-view-${option.value}`}
              className={cn(
                "rounded-[4px] px-2.5 py-1 text-xs font-medium transition-colors",
                view === option.value
                  ? "bg-primary-soft text-on-primary-soft"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {option.label}
            </button>
          ))}
        </div>
        {summary ? <span className="text-xs text-muted-foreground">{summary}</span> : null}
      </div>
      {children ? <div className="flex flex-wrap items-center gap-2">{children}</div> : null}
    </div>
  );
}
