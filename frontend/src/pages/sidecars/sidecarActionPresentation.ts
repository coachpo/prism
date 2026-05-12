import type { BadgeIntent } from "@/components/StatusBadge";
import type { SidecarActionStatusLabel, SidecarActionTypeLabel } from "@/i18n/messages/en";

export type ActionStatusLabels = Record<SidecarActionStatusLabel, string>;
export type ActionTypeLabels = Record<SidecarActionTypeLabel, string>;

const PROBE_ERROR_PREFIX = "probe_failed_";
const PROBE_SKIPPED_ACTION = "probe_skipped_unsupported_provider";

function isKnownActionStatus(status: string): status is SidecarActionStatusLabel {
  return status === "succeeded" || status === "success" || status === "skipped" || status === "failed" || status === "error";
}

function actionTypeLabelKey(actionType: string): SidecarActionTypeLabel | null {
  if (actionType === "probe_succeeded") {
    return "probe_succeeded";
  }
  if (actionType === PROBE_SKIPPED_ACTION) {
    return "probe_skipped";
  }
  if (actionType.startsWith(PROBE_ERROR_PREFIX)) {
    return "probe_error";
  }
  if (actionType === "quota_hold_extended") {
    return "quota_hold_extended";
  }
  return null;
}

export function isProbeActionType(actionType: string) {
  return actionType === "probe_succeeded" || actionType === PROBE_SKIPPED_ACTION || actionType.startsWith(PROBE_ERROR_PREFIX);
}

export function formatActionStatus(status: string, labels: ActionStatusLabels) {
  return isKnownActionStatus(status) ? labels[status] : status;
}

export function formatActionType(actionType: string, labels: ActionTypeLabels) {
  const labelKey = actionTypeLabelKey(actionType);
  return labelKey ? labels[labelKey] : actionType;
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
  if (actionType === PROBE_SKIPPED_ACTION || actionType === "quota_hold_extended" || actionType.includes("deprioritize")) {
    return "warning";
  }
  if (actionType.startsWith(PROBE_ERROR_PREFIX)) {
    return "danger";
  }
  if (actionType.includes("operator")) {
    return "accent";
  }
  return "info";
}
