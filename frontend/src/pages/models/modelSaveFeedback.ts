import { getStaticMessages } from "@/i18n/staticMessages";
import { ApiError } from "@/lib/api/request";

/** Keep server diagnostics in engineering evidence, never in a model form. */
export function getModelSaveErrorMessage(error: unknown): string {
  const copy = getStaticMessages().modelsData;
  if (error instanceof ApiError) {
    if (error.status === 409) return copy.saveConflict;
    if (error.status === 400 || error.status === 422) return copy.saveInvalid;
  }
  return copy.saveFailed;
}
