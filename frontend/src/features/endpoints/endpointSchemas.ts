import { z } from "zod"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { EndpointCreate, EndpointUpdate } from "@/lib/types"

// Server contract parity: name 1..128 Unicode code points after trim;
// normalized base URL <=512 code points with http/https scheme + host.
const MAX_NAME_CODE_POINTS = 128
const MAX_BASE_URL_CODE_POINTS = 512

// 校验消息与表单其余文案同源：界面是简体中文单一 locale，
// 提交后弹出的英文串正好出现在操作者最需要读懂的时刻。
const schemaCopy = getStaticMessages().endpointsUi

export const endpointFormSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, schemaCopy.nameRequired)
    .max(MAX_NAME_CODE_POINTS, schemaCopy.nameTooLong(MAX_NAME_CODE_POINTS)),
  base_url: z
    .string()
    .trim()
    .pipe(z.url(schemaCopy.baseUrlInvalid)),
  api_key: z.string(),
})

export type EndpointFormValues = z.input<typeof endpointFormSchema>
export type EndpointFormPayload = z.output<typeof endpointFormSchema>

export const ENDPOINT_FORM_DEFAULT_VALUES: EndpointFormValues = {
  name: "",
  base_url: "",
  api_key: "",
}

export function normalizeEndpointFormValues(values: EndpointFormValues): EndpointFormPayload {
  return endpointFormSchema.parse(values)
}

// canonicalBaseURLPreview mirrors the server normalization order: trim
// surrounding whitespace, then strip trailing slashes while preserving a valid
// origin form. It never mutates user keystrokes; the server response remains
// the authority on submit.
export function canonicalBaseURLPreview(rawValue: string): string {
  const trimmed = rawValue.trim()
  if (!trimmed) return ""
  const withoutSlash = trimmed.replace(/\/+$/, "")
  try {
    const parsed = new URL(withoutSlash)
    if (parsed.protocol === "http:" || parsed.protocol === "https:") {
      return withoutSlash
    }
  } catch {
    // fall through to the trimmed form so validation reports the real problem
  }
  return trimmed
}

export function buildEndpointCreatePayload(values: EndpointFormValues): EndpointCreate {
  const parsed = normalizeEndpointFormValues(values)
  return {
    name: parsed.name,
    base_url: canonicalBaseURLPreview(parsed.base_url),
    api_key: parsed.api_key.trim(),
  }
}

export function buildEndpointUpdatePayload(values: EndpointFormValues, expectedUpdatedAt?: string): EndpointUpdate {
  const parsed = normalizeEndpointFormValues(values)
  return {
    name: parsed.name,
    base_url: canonicalBaseURLPreview(parsed.base_url),
    ...(parsed.api_key.trim() ? { api_key: parsed.api_key.trim() } : {}),
    ...(expectedUpdatedAt ? { expected_updated_at: expectedUpdatedAt } : {}),
  }
}

export type ReviewFilterValue = "all" | "referenced" | "unreferenced" | "inactive_only"

export function hasEndpointReviewFilters(options: { searchQuery: string; reviewFilter: ReviewFilterValue }) {
  return options.searchQuery.trim().length > 0 || options.reviewFilter !== "all"
}

export { MAX_BASE_URL_CODE_POINTS, MAX_NAME_CODE_POINTS }
