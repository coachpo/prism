import type { BadgeIntent } from "@/components/StatusBadge";
import type { SidecarActionStatusLabel, SidecarActionTypeLabel } from "@/i18n/messages/en";

export type ActionStatusLabels = Record<SidecarActionStatusLabel, string>;
export type ActionTypeLabels = Record<SidecarActionTypeLabel, string>;

const KNOWN_ACTION_TYPES = new Set<string>([
  "probe_succeeded",
  "probe_failed_timeout",
  "probe_failed_management_auth",
  "probe_failed_token",
  "probe_failed_status",
  "probe_failed_parse",
  "probe_failed_transport",
  "probe_skipped_unsupported_provider",
  "quota_hold_extended",
]);

function isKnownActionStatus(status: string): status is SidecarActionStatusLabel {
  return status === "succeeded" || status === "success" || status === "skipped" || status === "failed" || status === "error";
}

export function isKnownActionType(actionType: string): actionType is SidecarActionTypeLabel {
  return KNOWN_ACTION_TYPES.has(actionType);
}

export function isProbeActionType(actionType: string) {
  return actionType === "probe_succeeded" || actionType === "probe_skipped_unsupported_provider" || actionType.startsWith("probe_failed_");
}

export function formatActionStatus(status: string, labels: ActionStatusLabels) {
  return isKnownActionStatus(status) ? labels[status] : status;
}

export function formatActionType(actionType: string, labels: ActionTypeLabels) {
  return isKnownActionType(actionType) ? labels[actionType] : actionType;
}

export function sidecarStatusIntent(status: string): BadgeIntent {
  if (status === "succeeded" || status === "success") {
    return "success";
  }
  if (status === "skipped") {
    return "warning";
  }
  if (status === "failed" || status === "error") {
    return "danger";
  }
  return "muted";
}

export function sidecarActionIntent(actionType: string): BadgeIntent {
  if (actionType === "probe_succeeded" || actionType.includes("restore")) {
    return "success";
  }
  if (actionType === "probe_skipped_unsupported_provider" || actionType === "quota_hold_extended" || actionType.includes("deprioritize")) {
    return "warning";
  }
  if (actionType.startsWith("probe_failed_")) {
    return "danger";
  }
  if (actionType.includes("operator")) {
    return "accent";
  }
  return "info";
}
