/**
 * Operation-aware curl builder (Proxy Key SPEC §8.3/§8.4).
 *
 * Every template is non-streaming, uses the fixed non-sensitive prompt
 * "Reply with exactly OK.", requests a very small output and carries the
 * selected facade model. The secret is always a header value and never enters
 * the URL, query or body.
 */

import type { ApiFamily } from "@/lib/types";
import type { OpenAIAcceptedFormat } from "@/lib/types";
import { buildRuntimeUrl } from "./effectiveOrigin";

export const SELF_TEST_PROMPT = "Reply with exactly OK.";
export const SELF_TEST_MAX_OUTPUT_TOKENS = 8;

export type OpenAIOperation = "chat_completions" | "responses";

export interface CurlBuildInput {
  apiFamily: ApiFamily;
  openaiAcceptedFormat: OpenAIAcceptedFormat | null;
  modelId: string;
  proxyKey: string;
  openaiOperation?: OpenAIOperation;
}

export interface CurlBuildOutput {
  /** Exact runtime operation URL (authoritative for copying and self-test). */
  url: string;
  /** Convenience family route base (not a promise of arbitrary paths). */
  familyBaseUrl: string;
  gatewayOrigin: string;
  operation: OpenAIOperation | "messages" | "generate_content";
  curl: string;
  headers: Record<string, string>;
  body: string;
}

function posixShellQuote(value: string): string {
  // Single-quote escaping: ' -> '\'' — the only POSIX-safe quoting for
  // arbitrary Unicode/whitespace/long model ids.
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function openaiOperationFor(
  acceptedFormat: OpenAIAcceptedFormat | null,
  explicit?: OpenAIOperation,
): OpenAIOperation {
  if (explicit !== undefined) {
    return explicit;
  }
  if (acceptedFormat === "chat_completions_only") {
    return "chat_completions";
  }
  // responses_only and dual_native default to Responses.
  return "responses";
}

function openaiPath(operation: OpenAIOperation): string {
  return operation === "chat_completions" ? "/v1/chat/completions" : "/v1/responses";
}

function openaiBody(modelId: string, operation: OpenAIOperation): string {
  if (operation === "chat_completions") {
    return JSON.stringify({
      model: modelId,
      messages: [{ role: "user", content: SELF_TEST_PROMPT }],
      stream: false,
      max_completion_tokens: SELF_TEST_MAX_OUTPUT_TOKENS,
    });
  }
  return JSON.stringify({
    model: modelId,
    input: SELF_TEST_PROMPT,
    stream: false,
    max_output_tokens: SELF_TEST_MAX_OUTPUT_TOKENS,
  });
}

function anthropicBody(modelId: string): string {
  return JSON.stringify({
    model: modelId,
    max_tokens: SELF_TEST_MAX_OUTPUT_TOKENS,
    stream: false,
    messages: [{ role: "user", content: SELF_TEST_PROMPT }],
  });
}

function geminiBody(): string {
  return JSON.stringify({
    contents: [{ role: "user", parts: [{ text: SELF_TEST_PROMPT }] }],
    generationConfig: { maxOutputTokens: SELF_TEST_MAX_OUTPUT_TOKENS },
  });
}

/** Encodes the model id as one route segment per the runtime route contract. */
export function encodeGeminiModelSegment(modelId: string): string {
  return encodeURIComponent(modelId);
}

export function buildSelfTestCurl(input: CurlBuildInput): CurlBuildOutput {
  const { apiFamily, proxyKey } = input;
  const gatewayOrigin = buildRuntimeUrl("/").origin;
  const commonHeaders: Record<string, string> = {
    "Content-Type": "application/json",
  };

  const modelId = input.modelId;
  if (apiFamily === "anthropic") {
    const url = buildRuntimeUrl("/v1/messages").toString();
    const headers = {
      ...commonHeaders,
      "X-API-Key": proxyKey,
      "anthropic-version": "2023-06-01",
    };
    const body = anthropicBody(input.modelId);
    return {
      url,
      familyBaseUrl: `${gatewayOrigin}/v1`,
      gatewayOrigin,
      operation: "messages",
      headers,
      body,
      curl: buildCurlCommand("POST", url, headers, body),
    };
  }

  if (apiFamily === "gemini") {
    const path = `/v1beta/models/${encodeGeminiModelSegment(modelId)}:generateContent`;
    const url = buildRuntimeUrl(path).toString();
    const headers = { ...commonHeaders, "X-Goog-Api-Key": proxyKey };
    const body = geminiBody();
    return {
      url,
      familyBaseUrl: `${gatewayOrigin}/v1beta`,
      gatewayOrigin,
      operation: "generate_content",
      headers,
      body,
      curl: buildCurlCommand("POST", url, headers, body),
    };
  }

  // OpenAI family (including dual_native).
  const operation = openaiOperationFor(input.openaiAcceptedFormat, input.openaiOperation);
  const path = openaiPath(operation);
  const url = buildRuntimeUrl(path).toString();
  const headers = { ...commonHeaders, Authorization: `Bearer ${proxyKey}` };
  const body = openaiBody(modelId, operation);
  return {
    url,
    familyBaseUrl: `${gatewayOrigin}/v1`,
    gatewayOrigin,
    operation,
    headers,
    body,
    curl: buildCurlCommand("POST", url, headers, body),
  };
}

function buildCurlCommand(method: string, url: string, headers: Record<string, string>, body: string): string {
  const parts = [`curl ${method} ${posixShellQuote(url)}`];
  for (const [name, value] of Object.entries(headers)) {
    parts.push(`  -H ${posixShellQuote(`${name}: ${value}`)}`);
  }
  parts.push(`  -d ${posixShellQuote(body)}`);
  return parts.join(" \\\n");
}
