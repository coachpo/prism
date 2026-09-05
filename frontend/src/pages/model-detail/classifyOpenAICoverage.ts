import type { OpenAIAcceptedFormat, OpenAITextCapability } from "@/lib/types"

// Pure directional 3x3 coverage classification shared by the capability
// picker's immediate preview. An absent mode on either side means "serves no
// text operation" and never stands in for the full set. Backend diagnostics
// remain authoritative; this helper never claims to be a recursive graph
// result.

export type OpenAICoverageClass = "full" | "partial" | "none"

export const OPENAI_CHAT_COMPLETIONS_OPERATION = "openai.chat_completions"

export const OPENAI_RESPONSES_OPERATIONS = [
  "openai.responses",
  "openai.responses.input_tokens",
  "openai.responses.compact",
] as const

export function openaiAcceptedOperationSet(format: OpenAIAcceptedFormat | null | undefined): string[] {
  switch (format) {
    case "chat_completions_only":
      return [OPENAI_CHAT_COMPLETIONS_OPERATION]
    case "responses_only":
      return [...OPENAI_RESPONSES_OPERATIONS]
    case "dual_native":
      return [OPENAI_CHAT_COMPLETIONS_OPERATION, ...OPENAI_RESPONSES_OPERATIONS]
    default:
      return []
  }
}

export function openaiTargetSupportedOperationSet(capability: OpenAITextCapability | null | undefined): string[] {
  switch (capability) {
    case "chat_completions_only":
      return [OPENAI_CHAT_COMPLETIONS_OPERATION]
    case "responses_only":
      return [...OPENAI_RESPONSES_OPERATIONS]
    case "dual_native":
      return [OPENAI_CHAT_COMPLETIONS_OPERATION, ...OPENAI_RESPONSES_OPERATIONS]
    default:
      return []
  }
}

export interface OpenAICoverageClassification {
  coverage: OpenAICoverageClass
  supportedOperations: string[]
  unsupportedAcceptedOperations: string[]
}

export function classifyOpenAICoverage(
  modelAcceptedFormat: OpenAIAcceptedFormat | null | undefined,
  targetCapability: OpenAITextCapability | null | undefined,
): OpenAICoverageClassification {
  const accepted = openaiAcceptedOperationSet(modelAcceptedFormat)
  const supported = openaiTargetSupportedOperationSet(targetCapability)
  // An image-only owner accepts no text operation, so it leaves none unserved.
  if (accepted.length === 0) {
    return { coverage: "full", supportedOperations: [], unsupportedAcceptedOperations: [] }
  }
  const supportedSet = new Set(supported)
  const supportedOperations = accepted.filter((operation) => supportedSet.has(operation))
  const unsupportedAcceptedOperations = accepted.filter((operation) => !supportedSet.has(operation))
  if (supportedOperations.length === 0) {
    return { coverage: "none", supportedOperations, unsupportedAcceptedOperations }
  }
  if (unsupportedAcceptedOperations.length === 0) {
    return { coverage: "full", supportedOperations, unsupportedAcceptedOperations }
  }
  return { coverage: "partial", supportedOperations, unsupportedAcceptedOperations }
}
