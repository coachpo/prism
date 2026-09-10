import { getStaticMessages } from "@/i18n/staticMessages";

// Catalog failures carry bounded categories. Do not show remote response text
// or transport URLs; other errors (including CAS) keep their owning message.
export function catalogFailureMessage(cause: unknown): string {
 const text = cause instanceof Error ? cause.message : String(cause);
 const match = /(?:models_dev|pi)_catalog_unavailable:\s*([a-z_]+)/.exec(text);
 if (!match) return text;
 const copy = getStaticMessages().clientReadiness.failures;
 return (copy as Record<string, string>)[match[1]] ?? copy.unavailable;
}
