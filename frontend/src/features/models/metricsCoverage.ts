/**
 * 成本列写着「近 30 天」，但保留期可能只覆盖其中几天。后端在 coverage 里
 * 说明了这件事，页面必须消费它：把 30 天的裁剪结果当成 30 天的花费，
 * 预算和比价会差一个数量级。
 */
export interface SpendingCoverageClip {
  /** 实际生效窗口的起点（ISO），来自 retention_from_time 或最早的裁剪缺口。 */
  retentionFrom: string | null;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : null;
}

/**
 * 只有「因保留期删除而不完整」才算裁剪。其它原因的不完整（例如样本缺失）
 * 由各自的徽章表达，不在这里混为一谈。
 */
export function readSpendingRetentionClip(
  coverage: { spending: Record<string, unknown> } | null | undefined,
): SpendingCoverageClip | null {
  const spending = asRecord(coverage?.spending);
  if (!spending || spending.complete !== false) return null;

  const gaps = Array.isArray(spending.gaps) ? spending.gaps : [];
  const retentionGaps = gaps
    .map(asRecord)
    .filter(
      (gap): gap is Record<string, unknown> =>
        gap !== null && gap.reason === "retention_deleted",
    );
  if (retentionGaps.length === 0) return null;

  const retentionFrom =
    typeof spending.retention_from_time === "string"
      ? spending.retention_from_time
      : retentionGaps
          .map((gap) => (typeof gap.to === "string" ? gap.to : null))
          .filter((value): value is string => value !== null)
          .sort()
          .at(-1) ?? null;

  return { retentionFrom };
}
