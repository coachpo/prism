import { useLocale } from "@/i18n/useLocale";
import { cn, truncateIdentifier } from "@/lib/utils";
import { OperatorMissingValue } from "@/shared/design-system";
import { CopyButton } from "@/components/CopyButton";

/** Renders a retained upstream identity without inferring a missing snapshot. */
export function UpstreamModelIdValue({
  value,
  missingReason,
  showLabel = false,
  copyable = false,
  elide = false,
  className,
  testId,
}: {
  value: string | null;
  missingReason: string;
  showLabel?: boolean;
  /**
   * 挂一个悬停/聚焦才显形的复制控件。放在 `<button>` 行里的调用点必须留 false：
   * 按钮里再嵌按钮不是合法 HTML。
   */
  copyable?: boolean;
  /**
   * 表格标识符列专用：中段省略。详情面板有的是宽度，那里必须给全值。
   */
  elide?: boolean;
  className?: string;
  testId?: string;
}) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;
  // 尾部省略恰好砍掉模型 ID 的区分位：codex/codex-auto-review 与
  // codex/codex-auto-review-2 在表里长得一模一样。表里的标识符走中段省略。
  const elided = value === null ? null : elide ? truncateIdentifier(value) : value;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-0.5", className)}>
      <span className="min-w-0 truncate font-mono" data-testid={testId} title={value ?? undefined}>
        {showLabel ? `${copy.upstreamModelIdColumn}: ` : null}
        {elided ?? <OperatorMissingValue reason={missingReason} />}
      </span>
      {copyable && value !== null ? (
        <CopyButton
          value={value}
          label=""
          targetLabel={copy.upstreamModelIdColumn}
          aria-label={copy.copyValueAria(copy.upstreamModelIdColumn)}
          size="icon-sm"
          stopPropagation
          className="shrink-0 opacity-0 transition-opacity focus-visible:opacity-100 group-hover/row:opacity-100 [@media(hover:none)]:opacity-100"
        />
      ) : null}
    </span>
  );
}
