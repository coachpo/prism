import { useMemo, useState } from "react";
import { Columns3 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { getColumns } from "./columns";
import { allColumnKeys } from "./requestLogColumnPreferences";

// Column visibility toggle (Requests SPEC §9.3): a compact popover listing
// all column keys with checkboxes and a reset-to-defaults action. Keeps the
// pricing_state column always available; hiding it is allowed but it remains
// part of the default set.
export function ColumnToggleMenu({
  visibleColumns,
  onToggleColumn,
  onResetColumns,
}: {
  visibleColumns: string[];
  onToggleColumn: (key: string) => void;
  onResetColumns: () => void;
}) {
  const [open, setOpen] = useState(false);
  const { messages } = useLocale();
  const keys = allColumnKeys();
  const labels = useMemo(() => {
    const byKey = new Map(
      getColumns().map((column) => [column.key, column.label]),
    );
    return byKey;
  }, []);

  return (
    <div className="relative">
      <Button
        variant="outline"
        size="sm"
        className="h-8 gap-1.5 text-xs"
        aria-expanded={open}
        aria-haspopup="menu"
        data-testid="request-log-column-toggle-trigger"
        onClick={() => setOpen((current) => !current)}
      >
        <Columns3 className="h-3.5 w-3.5" />
        {messages.requestLogs.allColumns}
      </Button>
      {open ? (
        <>
          <button
            type="button"
            aria-label="关闭列选择"
            className="fixed inset-0 z-40 cursor-default"
            onClick={() => setOpen(false)}
          />
          <div
            role="menu"
            data-testid="request-log-column-toggle"
            className="absolute right-0 z-50 mt-1 max-h-80 w-56 overflow-auto rounded-lg border border-border bg-panel p-2"
          >
            {keys.map((key) => {
              const checked = visibleColumns.includes(key);
              return (
                <label
                  key={key}
                  role="menuitemcheckbox"
                  aria-checked={checked}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-xs text-foreground hover:bg-inset"
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => onToggleColumn(key)}
                    className="size-3.5 accent-primary"
                  />
                  <span className="truncate">{labels.get(key) ?? key}</span>
                </label>
              );
            })}
            <div className="mt-1 border-t border-border pt-1">
              <button
                type="button"
                className="w-full rounded-md px-2 py-1.5 text-left text-xs font-medium text-primary hover:bg-inset"
                onClick={() => {
                  onResetColumns();
                  setOpen(false);
                }}
              >
                {messages.requestLogs.allColumns ?? "恢复默认列"}
              </button>
            </div>
          </div>
        </>
      ) : null}
    </div>
  );
}
