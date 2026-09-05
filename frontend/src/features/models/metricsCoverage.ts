import type {
  StatsCoverageByDataset,
  StatsDatasetCoverage,
} from "@/lib/types/model-stats";

/**
 * 成本列写着「近 30 天」，但保留期可能只覆盖其中几天。后端在 coverage 里
 * 说明了这件事，页面必须消费它：把 30 天的裁剪结果当成 30 天的花费，
 * 预算和比价会差一个数量级。
 */
export interface SpendingCoverageClip {
  /** 实际生效窗口的起点（ISO），来自 retention_from_time 或最早的裁剪缺口。 */
  retentionFrom: string | null;
}

/** coverage 按数据集嵌套，父层没有 complete —— 必须逐个数据集看。 */
function datasetsOf(
  coverage: StatsCoverageByDataset | null | undefined,
): StatsDatasetCoverage[] {
  if (typeof coverage !== "object" || coverage === null) return [];
  return Object.values(coverage).filter(
    (dataset): dataset is StatsDatasetCoverage =>
      typeof dataset === "object" && dataset !== null,
  );
}

/**
 * 只有「因保留期删除而不完整」才算裁剪。其它原因的不完整（例如样本缺失）
 * 由各自的徽章表达，不在这里混为一谈。
 */
export function readSpendingRetentionClip(
  coverage: { spending?: StatsCoverageByDataset | null } | null | undefined,
): SpendingCoverageClip | null {
  const clipped = datasetsOf(coverage?.spending).filter(
    (dataset) =>
      dataset.complete === false &&
      Array.isArray(dataset.gaps) &&
      dataset.gaps.some((gap) => gap?.reason === "retention_deleted"),
  );
  if (clipped.length === 0) return null;

  // 多个数据集都被裁剪时取最晚的那个起点：那才是所有列都拿得到数据的时刻。
  const candidates = clipped.map((dataset) => {
    if (typeof dataset.retention_from_time === "string") {
      return dataset.retention_from_time;
    }
    return (
      dataset.gaps
        .filter((gap) => gap?.reason === "retention_deleted")
        .map((gap) => (typeof gap.to_time === "string" ? gap.to_time : null))
        .filter((value): value is string => value !== null)
        .sort()
        .at(-1) ?? null
    );
  });

  const retentionFrom =
    candidates
      .filter((value): value is string => value !== null)
      .sort()
      .at(-1) ?? null;

  return { retentionFrom };
}
