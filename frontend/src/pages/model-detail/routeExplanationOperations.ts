import type { Messages } from "@/i18n/messages";
import { OPENAI_CHAT_COMPLETIONS_OPERATION, OPENAI_RESPONSES_OPERATIONS } from "./classifyOpenAICoverage";

// These are the existing model-bound native operations; options do not imply
// that a particular model accepts an operation. Runtime remains authoritative.
export function routeExplanationOperations(apiFamily: string, messages: Messages) {
  const copy = messages.observe;
  const explanation = messages.routeExplanation;
  const labels: Record<string, string> = {
    [OPENAI_CHAT_COMPLETIONS_OPERATION]: copy.routingChatLabel,
    [OPENAI_RESPONSES_OPERATIONS[0]]: copy.routingResponsesLabel,
    [OPENAI_RESPONSES_OPERATIONS[1]]: explanation.responsesInputTokens,
    [OPENAI_RESPONSES_OPERATIONS[2]]: explanation.responsesCompact,
    "openai.images.generations": copy.imagesGenerations,
    "openai.images.edits": copy.imagesEdits,
    "anthropic.messages": copy.routingAnthropicMessagesLabel,
    "anthropic.count_tokens": copy.routingAnthropicCountTokensLabel,
    "gemini.generate_content": copy.routingGeminiGenerateContentLabel,
    "gemini.stream_generate_content": copy.routingGeminiStreamGenerateContentLabel,
    "gemini.count_tokens": copy.routingGeminiCountTokensLabel,
  };
  return Object.entries(labels).filter(([name]) => name.startsWith(`${apiFamily}.`));
}
