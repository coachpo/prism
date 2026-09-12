import { getStaticMessages } from "@/i18n/staticMessages"

/** Only locally authored messages reach the UI; the response body stays in ApiError.detail. */
export function managementErrorMessage(status: number): string {
  const copy = getStaticMessages().common.requestErrors
  if (status === 400 || status === 422) return copy.invalid
  if (status === 401) return copy.signIn
  if (status === 403) return copy.denied
  if (status === 404 || status === 410) return copy.missing
  if (status === 409 || status === 412) return copy.changed
  if (status === 413) return copy.tooLarge
  if (status === 429) return copy.busy
  if (status >= 500) return copy.unavailable
  return copy.unknown
}
