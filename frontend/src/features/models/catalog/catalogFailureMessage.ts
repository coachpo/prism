import { getStaticMessages } from "@/i18n/staticMessages";
import { managementErrorMessage } from "@/lib/api/errorMessage";

// Preserve translated catalog outcomes; diagnostic strings never become copy.
export function catalogFailureMessage(cause: unknown): string {
 const text = cause instanceof Error ? cause.message : String(cause);
 const messages = getStaticMessages();
 const match = /(?:models_dev|pi)_catalog_unavailable:\s*([a-z_]+)/.exec(text);
 if (match) {
  const copy = messages.clientReadiness.failures;
  return (copy as Record<string, string>)[match[1]] ?? copy.unavailable;
 }
 if (text.startsWith("pi_catalog_search_identity_changed:")) return messages.modelExportPage.directorySearchEvidenceChanged;
 if (typeof cause === "object" && cause !== null && "status" in cause && typeof cause.status === "number") return managementErrorMessage(cause.status);
 const authored = [messages.modelCatalog, messages.modelExportPage, messages.common.requestErrors].flatMap(Object.values);
 return authored.includes(text) ? text : messages.common.requestErrors.unknown;
}
