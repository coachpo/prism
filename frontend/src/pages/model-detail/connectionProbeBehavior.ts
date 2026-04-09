import type { OpenAIProbeEndpointVariant } from "@/lib/types";

export type OpenAIProbeApi = "responses" | "chat_completions";
export type OpenAIProbeReasoningMode = "default" | "disabled";

export const DEFAULT_OPENAI_PROBE_ENDPOINT_VARIANT: OpenAIProbeEndpointVariant = "responses_minimal";

export function resolveOpenAIProbeVariant({
  probeApi,
  reasoningMode,
}: {
  probeApi: OpenAIProbeApi;
  reasoningMode: OpenAIProbeReasoningMode;
}): OpenAIProbeEndpointVariant {
  if (probeApi === "responses") {
    return reasoningMode === "disabled" ? "responses_reasoning_none" : "responses_minimal";
  }

  return reasoningMode === "disabled"
    ? "chat_completions_reasoning_none"
    : "chat_completions_minimal";
}

export function decomposeOpenAIProbeVariant(
  variant: OpenAIProbeEndpointVariant | null | undefined,
): {
  probeApi: OpenAIProbeApi;
  reasoningMode: OpenAIProbeReasoningMode;
} {
  switch (variant) {
    case "responses_reasoning_none":
      return { probeApi: "responses", reasoningMode: "disabled" };
    case "chat_completions_minimal":
      return { probeApi: "chat_completions", reasoningMode: "default" };
    case "chat_completions_reasoning_none":
      return { probeApi: "chat_completions", reasoningMode: "disabled" };
    case "responses_minimal":
    default:
      return { probeApi: "responses", reasoningMode: "default" };
  }
}

export function normalizeOpenAIProbeEndpointVariant(
  variant: OpenAIProbeEndpointVariant | null | undefined,
): OpenAIProbeEndpointVariant {
  return variant ?? DEFAULT_OPENAI_PROBE_ENDPOINT_VARIANT;
}
