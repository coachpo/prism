import { useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";

/** Narrow screens reach request summaries first; all filters retain their mounted state. */
export function RequestResponsiveFilters({
  children,
}: {
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const { messages } = useLocale();
  return (
    <div className="min-w-0 flex flex-col gap-2">
      <Button
        variant="outline"
        className="md:hidden self-start"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        {messages.requestComparison.filters}
      </Button>
      <div className={open ? "min-w-0" : "hidden min-w-0 md:block"}>
        {children}
      </div>
    </div>
  );
}
