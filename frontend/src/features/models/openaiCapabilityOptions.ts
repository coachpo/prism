import type { OpenAIAcceptedFormat, OpenAIImageOperations } from "@/lib/types"
import { getStaticMessages } from "@/i18n/staticMessages"

/**
 * Select controls cannot carry an empty option value, so both OpenAI capability
 * dimensions use this sentinel for "this row does not serve that dimension".
 * It never crosses the API boundary: callers map it back to null before
 * building a payload.
 */
export const OPENAI_CAPABILITY_UNSET = "__unset__" as const

export type OpenAICapabilitySelectValue<T extends string> = T | typeof OPENAI_CAPABILITY_UNSET

type ModelsUiCopy = ReturnType<typeof getStaticMessages>["modelsUi"]

export function getOpenAIAcceptedFormatOptionLabel(
  value: OpenAICapabilitySelectValue<OpenAIAcceptedFormat>,
  copy: ModelsUiCopy,
): string {
  switch (value) {
    case "responses_only":
      return copy.openaiAcceptedFormatResponsesOnly
    case "chat_completions_only":
      return copy.openaiAcceptedFormatChatCompletionsOnly
    case "dual_native":
      return copy.openaiAcceptedFormatDualNative
    default:
      return copy.openaiAcceptedFormatNone
  }
}

export function getOpenAIImageOperationsOptionLabel(
  value: OpenAICapabilitySelectValue<OpenAIImageOperations>,
  copy: ModelsUiCopy,
): string {
  switch (value) {
    case "generations":
      return copy.openaiImageOperationsGenerations
    case "edits":
      return copy.openaiImageOperationsEdits
    case "generations_and_edits":
      return copy.openaiImageOperationsGenerationsAndEdits
    default:
      return copy.openaiImageOperationsNone
  }
}

/** Select values for the text dimension, "unset" first. */
export const OPENAI_ACCEPTED_FORMAT_SELECT_VALUES: readonly OpenAICapabilitySelectValue<OpenAIAcceptedFormat>[] = [
  OPENAI_CAPABILITY_UNSET,
  "dual_native",
  "responses_only",
  "chat_completions_only",
]

/** Select values for the image dimension, "unset" first. */
export const OPENAI_IMAGE_OPERATIONS_SELECT_VALUES: readonly OpenAICapabilitySelectValue<OpenAIImageOperations>[] = [
  OPENAI_CAPABILITY_UNSET,
  "generations",
  "edits",
  "generations_and_edits",
]

export function toSelectValue<T extends string>(value: T | "" | null | undefined): OpenAICapabilitySelectValue<T> {
  return value === "" || value === null || value === undefined ? OPENAI_CAPABILITY_UNSET : value
}

export function fromSelectValue<T extends string>(value: string): T | "" {
  return value === OPENAI_CAPABILITY_UNSET ? "" : (value as T)
}
