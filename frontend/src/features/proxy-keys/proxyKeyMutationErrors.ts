import { toast } from "sonner";

export function showProxyKeyMutationError(error: unknown, fallback: string) {
  toast.error(error instanceof Error ? error.message : fallback);
}
