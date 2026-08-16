import { toast } from "sonner";
import { copyTextToClipboard } from "@/lib/clipboard";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

export function getStatusIntent(statusCode: number) {
  if (statusCode >= 200 && statusCode < 300) return "healthy" as const;
  if (statusCode >= 400 && statusCode < 500) return "degraded" as const;
  return "failing" as const;
}

export function getStatusTone(statusCode: number) {
  if (statusCode >= 200 && statusCode < 300) {
    return { card: "border-l-healthy bg-healthy/5" };
  }

  if (statusCode >= 400 && statusCode < 500) {
    return { card: "border-l-degraded bg-degraded/5" };
  }

  return { card: "border-l-failing bg-failing/5" };
}

export async function copyRequestLogText(content: string, label: string, container?: HTMLElement | null) {
  const copied = await copyTextToClipboard(content, container);
  if (copied) {
    toast.success(getMessages().requestLogsDetail.copied(label));
    return;
  }

  toast.error(getMessages().requestLogsDetail.copyFailed(label));
}
