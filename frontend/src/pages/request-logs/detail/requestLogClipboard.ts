import { toast } from "sonner";
import { copyTextToClipboard } from "@/lib/clipboard";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

export async function copyRequestLogText(content: string, label: string, container?: HTMLElement | null) {
  const copied = await copyTextToClipboard(content, container);
  if (copied) {
    toast.success(getMessages().requestLogsDetail.copied(label));
    return;
  }

  toast.error(getMessages().requestLogsDetail.copyFailed(label));
}
