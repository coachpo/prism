import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { cn } from "@/lib/utils";

export type DataTableSortDirection = "asc" | "desc";

interface DataTableColumnHeaderProps {
  active: boolean;
  className?: string;
  direction: DataTableSortDirection;
  label: string;
  onToggle?: () => void;
}

export function DataTableColumnHeader({
  active,
  className,
  direction,
  label,
  onToggle,
}: DataTableColumnHeaderProps) {
  if (!onToggle) {
    return <span className={cn("text-xs font-medium uppercase tracking-[0.18em]", className)}>{label}</span>;
  }

  return (
    <button
      aria-label={label}
      className={cn(
        "-ml-1 inline-flex items-center gap-1 rounded-md px-1 py-1 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        active && "text-foreground",
        className,
      )}
      onClick={onToggle}
      type="button"
    >
      <span>{label}</span>
      {active ? (
        direction === "asc" ? (
          <ArrowUp className="h-3.5 w-3.5" />
        ) : (
          <ArrowDown className="h-3.5 w-3.5" />
        )
      ) : (
        <ChevronsUpDown className="h-3.5 w-3.5 opacity-60" />
      )}
    </button>
  );
}
