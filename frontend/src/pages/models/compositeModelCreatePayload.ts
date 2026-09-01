import type {
  ApiFamily,
  ModelConfigCompositeCreate,
  OpenAIAcceptedFormat,
  OpenAIImageOperations,
} from "@/lib/types";

export interface CompositeModelCreatePayloadInput {
  apiFamily: ApiFamily;
  modelId: string;
  displayName: string;
  loadbalanceStrategyId: number;
  configureLater: boolean;
  openAIAcceptedFormat: OpenAIAcceptedFormat | null;
  openAIImageOperations: OpenAIImageOperations | null;
  initialTerminalTarget?: {
    endpoint_id?: number;
    endpoint_create?: {
      name: string;
      base_url: string;
      api_key: string;
    };
    name?: string | null;
    is_active?: boolean;
    /** Omit only for non-form callers that intentionally request API defaulting. */
    upstream_model_id?: string;
  };
}

/**
 * Shapes the one-step create wire contract. OpenAI-only keys are absent—not
 * null—on Anthropic and Gemini payloads because the backend validates field
 * presence before it validates values.
 */
export function buildCompositeModelCreatePayload(
  input: CompositeModelCreatePayloadInput,
): ModelConfigCompositeCreate {
  const common = {
    model_id: input.modelId.trim(),
    display_name: input.displayName.trim() || null,
    loadbalance_strategy_id: input.loadbalanceStrategyId,
    is_enabled: !input.configureLater,
  };
  const initialTarget = input.configureLater
    ? undefined
    : input.initialTerminalTarget;

  if (input.apiFamily === "openai") {
    return {
      ...common,
      api_family: "openai",
      openai_accepted_format: input.openAIAcceptedFormat,
      openai_image_operations: input.openAIImageOperations,
      ...(initialTarget
        ? {
            initial_terminal_target: {
              ...initialTarget,
              openai_text_capability: input.openAIAcceptedFormat,
              openai_image_capability: input.openAIImageOperations,
            },
          }
        : {}),
    };
  }

  return {
    ...common,
    api_family: input.apiFamily,
    ...(initialTarget ? { initial_terminal_target: initialTarget } : {}),
  };
}
