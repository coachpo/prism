import type { ApiFamily, OpenAIAcceptedFormat, OpenAITextCapability } from "@/lib/types"

// ModelCreatePayloadWithTarget is the composite create payload produced by the
// CreateModelDialog. The nested initial_terminal_target is optional: when the
// operator chooses "configure later" it is omitted entirely.
export interface ModelCreatePayloadWithTarget {
  api_family: ApiFamily
  model_id: string
  display_name: string
  loadbalance_strategy_id: number
  openai_accepted_format?: OpenAIAcceptedFormat
  is_enabled: boolean
  initial_terminal_target?: {
    endpoint_id?: number
    endpoint_create?: {
      name: string
      base_url: string
      api_key: string
    }
    name?: string
    openai_text_capability?: OpenAITextCapability
    pricing_template_id?: number
    qps_limit?: number
    max_in_flight_non_stream?: number
    max_in_flight_stream?: number
    custom_headers?: Record<string, string>
  }
}
