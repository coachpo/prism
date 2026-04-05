import { toast } from "sonner";
import { copyTextToClipboard } from "@/lib/clipboard";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

export function getStatusIntent(statusCode: number) {
  if (statusCode >= 200 && statusCode < 300) return "success" as const;
  if (statusCode >= 400 && statusCode < 500) return "warning" as const;
  return "danger" as const;
}

export function getStatusTone(statusCode: number) {
  if (statusCode >= 200 && statusCode < 300) {
    return { card: "border-l-emerald-500 bg-emerald-500/[0.05]" };
  }

  if (statusCode >= 400 && statusCode < 500) {
    return { card: "border-l-amber-500 bg-amber-500/[0.06]" };
  }

  return { card: "border-l-red-500 bg-red-500/[0.06]" };
}

export async function copyRequestLogText(content: string, label: string) {
  const copied = await copyTextToClipboard(content);
  if (copied) {
    toast.success(getMessages().requestLogsDetail.copied(label));
    return;
  }

  toast.error(getMessages().requestLogsDetail.copyFailed(label));
}
