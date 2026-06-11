import { z } from "zod"
import type { EndpointCreate, EndpointUpdate } from "@/lib/types"

export const endpointFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  base_url: z.string().trim().pipe(z.url("Must be a valid URL")),
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

export function buildEndpointCreatePayload(values: EndpointFormValues): EndpointCreate {
  const parsed = normalizeEndpointFormValues(values)
  return {
    name: parsed.name,
    base_url: parsed.base_url,
    api_key: parsed.api_key.trim(),
  }
}

export function buildEndpointUpdatePayload(values: EndpointFormValues): EndpointUpdate {
  const parsed = normalizeEndpointFormValues(values)
  return {
    name: parsed.name,
    base_url: parsed.base_url,
    ...(parsed.api_key.trim() ? { api_key: parsed.api_key.trim() } : {}),
  }
}

export function canReorderEndpoints(options: {
  endpointCount: number
  filtersActive: boolean
  reorderInFlight?: boolean
}) {
  return options.endpointCount > 1 && !options.filtersActive && !options.reorderInFlight
}

export function hasEndpointReviewFilters(options: { searchQuery: string; reviewFilter: "all" | "in-use" | "unused" }) {
  return options.searchQuery.trim().length > 0 || options.reviewFilter !== "all"
}
