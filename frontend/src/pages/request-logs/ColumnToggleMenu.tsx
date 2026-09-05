import { Columns3 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";

export interface ColumnToggleOption {
  key: string;
  label: string;
}

// Column visibility toggle (Requests SPEC §9.3). 面板走共享的 Radix 菜单原语：
// Esc 关闭、焦点归还、role=menuitemcheckbox、aria-expanded/aria-controls 都由它
// 提供，手搓浮层曾经在 Tab 序里凭空放进一个 1440×900 的隐形关闭按钮。
// 列表由调用方按当前视图给出，项名与表头同源，勾掉一列必然有视觉变化。
export function ColumnToggleMenu({
  columns,
  visibleColumns,
  onToggleColumn,
  onResetColumns,
}: {
  columns: ColumnToggleOption[];
  visibleColumns: string[];
  onToggleColumn: (key: string) => void;
  onResetColumns: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="h-8 gap-1.5 text-xs"
          data-testid="request-log-column-toggle-trigger"
        >
          <Columns3 className="h-3.5 w-3.5" />
          {copy.columnVisibility}
        </Button>
      </DropdownMenuTrigger>
      {/* 面板贴着视口底时里面还有一半列看不见也滚不出来：
          collisionPadding 让它翻到上方，可用高度由 Radix 算给内容。 */}
      <DropdownMenuContent
        align="end"
        collisionPadding={16}
        aria-label={copy.columnVisibilityMenuLabel}
        data-testid="request-log-column-toggle"
        className="max-h-[min(24rem,var(--radix-dropdown-menu-content-available-height))] w-56 overflow-y-auto"
      >
        {columns.map((column) => (
          <DropdownMenuCheckboxItem
            key={column.key}
            checked={visibleColumns.includes(column.key)}
            onSelect={(event) => {
              // 一次勾选一列，菜单不该在第一次点击后就关掉。
              event.preventDefault();
            }}
            onCheckedChange={() => onToggleColumn(column.key)}
          >
            <span className="truncate">{column.label}</span>
          </DropdownMenuCheckboxItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="text-primary"
          onSelect={() => onResetColumns()}
        >
          {copy.resetColumns}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
