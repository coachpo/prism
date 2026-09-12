import { getStaticMessages } from "@/i18n/staticMessages"

export function retentionDatasetLabel(dataset: string): string {
  const copy = getStaticMessages().settingsDialogs
  switch (dataset) {
    case "request_logs": return copy.cleanupTypeRequests
    case "usage_request_events": return copy.cleanupTypeStatistics
    case "audit_logs": return copy.cleanupTypeAudits
    case "loadbalance_events": return copy.cleanupTypeLoadbalanceEvents
    default: return copy.unknownDataset
  }
}

export function retentionWarningLabel(warning: string): string {
  const copy = getStaticMessages().settingsRetentionDeletion
  switch (warning) {
    case "延长保留期不会恢复已经清理的记录": return copy.deletionLimitNoRestore
    case "预览是提交前估算；delete-all 的最终 purge_to_time 由 worker 在 execution fence 锁定": return copy.deletionLimitEstimate
    case "手动清理不享受 scheduled query-token grace": return copy.deletionLimitImmediate
    default: return copy.deletionLimitUnrecognized
  }
}
