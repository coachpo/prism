import { useId } from "react";

import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

/**
 * 可选中的口径必须属于本页支持的集合；本页不支持的口径仍然渲染，
 * 但必须带上禁用理由 —— 禁用而不说明是不可接受的。
 */
export type MetricsScopeOption<TScope extends string> =
  | { value: TScope; label: string; basis: string; disabledReason?: never }
  | { value: string; label: string; basis: string; disabledReason: string };

/**
 * 所有 URL 驱动的统计口径切换共用这一个分段控件：列表页三档、详情页两档，
 * 键盘模型也只有一套。方向键即选中（radio 语义），而不是先移焦点再确认；
 * 可见标签与控件绑定，读屏与视觉拿到的是同一个名字。
 */
export function MetricsScopeSwitch<TScope extends string>({
  className,
  label,
  onChange,
  options,
  value,
}: {
  className?: string;
  label: string;
  onChange: (scope: TScope) => void;
  options: readonly MetricsScopeOption<TScope>[];
  value: TScope;
}) {
  const labelId = useId();

  const select = (next: string) => {
    const option = options.find((candidate) => candidate.value === next);
    if (!option || option.disabledReason || option.value === value) return;
    onChange(option.value as TScope);
  };

  return (
    <div className={cn("flex min-w-0 items-center gap-2", className)}>
      <span id={labelId} className="shrink-0 text-xs text-muted-foreground">
        {label}
      </span>
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        spacing={0}
        value={value}
        aria-labelledby={labelId}
        onValueChange={select}
      >
        {options.map((option) => (
          <ToggleGroupItem
            key={option.value}
            value={option.value}
            disabled={Boolean(option.disabledReason)}
            title={option.disabledReason ?? option.basis}
            onFocus={() => select(option.value)}
          >
            {option.label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </div>
  );
}
