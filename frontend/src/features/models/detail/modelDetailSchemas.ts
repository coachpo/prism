import { z } from "zod"

export const MODEL_DETAIL_TABS = ["connections", "events"] as const

export type ModelDetailTab = (typeof MODEL_DETAIL_TABS)[number]

export const DEFAULT_MODEL_DETAIL_TAB: ModelDetailTab = "connections"

// One-shot attach pair: action=create-terminal-target + a positive Endpoint ID.
// The schema accepts only this exact pair; anything else falls back to undefined
// so a refresh never reopens the dialog.
const ATTACH_ACTION = "create-terminal-target"
const positiveInteger = z.string().regex(/^[1-9]\d*$/)

export const modelDetailSearchSchema = z
  .object({
    tab: z.enum(MODEL_DETAIL_TABS).catch(DEFAULT_MODEL_DETAIL_TAB),
    focus_connection_id: z.string().regex(/^\d+$/).optional(),
    action: z.literal(ATTACH_ACTION).optional(),
    endpoint_id: positiveInteger.optional(),
  })
  .superRefine((value, context) => {
    const hasAction = value.action === ATTACH_ACTION
    const hasEndpointId = value.endpoint_id != null
    if (hasAction !== hasEndpointId) {
      context.addIssue({
        code: "custom",
        message: "action and endpoint_id must appear together",
      })
    }
  })

export type AttachTerminalTargetSearch = {
  action: typeof ATTACH_ACTION
  endpoint_id: number
}

export function parseAttachTerminalTargetSearch(search: {
  action?: string
  endpoint_id?: string
}): AttachTerminalTargetSearch | null {
  const parsed = modelDetailSearchSchema.safeParse(search)
  if (!parsed.success) return null
  const value = parsed.data
  if (value.action !== ATTACH_ACTION || value.endpoint_id == null) return null
  return { action: ATTACH_ACTION, endpoint_id: Number.parseInt(value.endpoint_id, 10) }
}

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

export function normalizeModelDetailTab(value: unknown): ModelDetailTab {
  return modelDetailSearchSchema.parse({ tab: value }).tab
}
