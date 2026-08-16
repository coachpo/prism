// Operation-aware streaming transcript (Requests SPEC §8.2/§8.3/§11.1):
// accumulates SSE events into a readable message transcript with tool calls
// and tool results. Covers OpenAI Chat Completions, OpenAI Responses,
// Anthropic Messages, and Gemini streams. Also extracts tool calls/results
// from non-streaming request/response bodies.
import type { SseEvent } from "./sseFraming";

export interface TranscriptTurn {
  role: string;
  text: string;
  toolCalls: TranscriptToolCall[];
  toolResults: TranscriptToolResult[];
  terminalState: string | null;
}

export interface TranscriptToolCall {
  index: number;
  name: string | null;
  argumentsJson: string | null;
  id: string | null;
}

export interface TranscriptToolResult {
  toolUseId: string | null;
  name: string | null;
  content: string | null;
  isError: boolean;
}

export interface TranscriptDocument {
  turns: TranscriptTurn[];
  usage: string | null;
  incomplete: boolean;
}

interface JsonRecord {
  [key: string]: unknown;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asRecord(value: unknown): JsonRecord | null {
  return isRecord(value) ? value : null;
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// ---------------------------------------------------------------------------
// Non-streaming extraction (request/response JSON bodies)
// ---------------------------------------------------------------------------

function extractOpenAiToolCalls(message: JsonRecord): TranscriptToolCall[] {
  const toolCalls = asArray(message.tool_calls);
  return toolCalls.flatMap((entry) => {
    const record = asRecord(entry);
    if (!record) return [];
    const fn = asRecord(record.function);
    return [{
      index: typeof record.index === "number" ? record.index : 0,
      name: fn ? asString(fn.name) || null : null,
      argumentsJson: fn ? (typeof fn.arguments === "string" ? fn.arguments : fn.arguments === undefined ? null : JSON.stringify(fn.arguments)) : null,
      id: asString(record.id) || null,
    }];
  });
}

function extractOpenAiToolResults(messages: unknown[]): TranscriptToolResult[] {
  return messages.flatMap((entry) => {
    const record = asRecord(entry);
    if (!record) return [];
    if (record.role === "tool") {
      return [{
        toolUseId: asString(record.tool_call_id) || null,
        name: null,
        content: typeof record.content === "string" ? record.content : JSON.stringify(record.content),
        isError: false,
      }];
    }
    return [];
  });
}

export function extractOpenAiRequestTools(body: JsonRecord): { calls: TranscriptToolCall[]; results: TranscriptToolResult[] } {
  const messages = asArray(body.messages);
  const calls: TranscriptToolCall[] = [];
  const results: TranscriptToolResult[] = [];
  for (const entry of messages) {
    const record = asRecord(entry);
    if (!record) continue;
    if (record.role === "assistant") {
      calls.push(...extractOpenAiToolCalls(record));
    } else if (record.role === "tool") {
      results.push(...extractOpenAiToolResults([record]));
    }
  }
  return { calls, results };
}

export function extractOpenAiResponseTools(body: JsonRecord): { calls: TranscriptToolCall[]; results: TranscriptToolResult[] } {
  const output = asArray(body.output);
  const calls: TranscriptToolCall[] = [];
  const results: TranscriptToolResult[] = [];
  for (const entry of output) {
    const record = asRecord(entry);
    if (!record) continue;
    const type = asString(record.type);
    if (type === "function_call") {
      const call = asRecord(record.call) ?? asRecord(record.function);
      const args = call ? asString(call.arguments) : "";
      calls.push({
        index: calls.length,
        name: call ? asString(call.name) || null : null,
        argumentsJson: args || null,
        id: asString(record.id) || asString(record.call_id) || null,
      });
    } else if (type === "function_call_output") {
      results.push({
        toolUseId: asString(record.call_id) || null,
        name: null,
        content: typeof record.output === "string" ? record.output : JSON.stringify(record.output),
        isError: false,
      });
    }
  }
  return { calls, results };
}

export function extractAnthropicTools(body: JsonRecord): { calls: TranscriptToolCall[]; results: TranscriptToolResult[] } {
  const messages = asArray(body.messages);
  const calls: TranscriptToolCall[] = [];
  const results: TranscriptToolResult[] = [];
  for (const entry of messages) {
    const record = asRecord(entry);
    if (!record) continue;
    for (const block of asArray(record.content)) {
      const blockRecord = asRecord(block);
      if (!blockRecord) continue;
      const type = asString(blockRecord.type);
      if (type === "tool_use") {
        calls.push({
          index: calls.length,
          name: asString(blockRecord.name) || null,
          argumentsJson: typeof blockRecord.input === "object" ? JSON.stringify(blockRecord.input) : asString(blockRecord.input) || null,
          id: asString(blockRecord.id) || null,
        });
      } else if (type === "tool_result") {
        results.push({
          toolUseId: asString(blockRecord.tool_use_id) || null,
          name: null,
          content: typeof blockRecord.content === "string" ? blockRecord.content : JSON.stringify(blockRecord.content),
          isError: blockRecord.is_error === true,
        });
      }
    }
  }
  return { calls, results };
}

export function extractGeminiTools(body: JsonRecord): { calls: TranscriptToolCall[]; results: TranscriptToolResult[] } {
  const contents = asArray(body.contents);
  const calls: TranscriptToolCall[] = [];
  const results: TranscriptToolResult[] = [];
  for (const entry of contents) {
    const record = asRecord(entry);
    if (!record) continue;
    for (const part of asArray(record.parts)) {
      const partRecord = asRecord(part);
      if (!partRecord) continue;
      const call = asRecord(partRecord.functionCall);
      if (call) {
        calls.push({
          index: calls.length,
          name: asString(call.name) || null,
          argumentsJson: typeof call.args === "object" ? JSON.stringify(call.args) : asString(call.args) || null,
          id: null,
        });
      }
      const response = asRecord(partRecord.functionResponse);
      if (response) {
        results.push({
          toolUseId: null,
          name: asString(response.name) || null,
          content: typeof response.response === "object" ? JSON.stringify(response.response) : asString(response.response),
          isError: false,
        });
      }
    }
  }
  return { calls, results };
}

// ---------------------------------------------------------------------------
// Streaming accumulation
// ---------------------------------------------------------------------------

export interface StreamAccumulatorState {
  turns: TranscriptTurn[];
  usage: string | null;
  incomplete: boolean;
}

function ensureTurn(state: StreamAccumulatorState, role: string): TranscriptTurn {
  const last = state.turns[state.turns.length - 1];
  if (last && last.role === role) return last;
  const turn: TranscriptTurn = { role, text: "", toolCalls: [], toolResults: [], terminalState: null };
  state.turns.push(turn);
  return turn;
}

// OpenAI Chat Completions SSE: choices[].delta{content, reasoning_content,
// tool_calls[]}, finish_reason, usage.
export function accumulateOpenAiChatSse(state: StreamAccumulatorState, event: SseEvent): void {
  const payload = asRecord(event.json);
  if (!payload) return;
  const choices = asArray(payload.choices);
  for (const choice of choices) {
    const choiceRecord = asRecord(choice);
    if (!choiceRecord) continue;
    const delta = asRecord(choiceRecord.delta);
    const finishReason = asString(choiceRecord.finish_reason);
    const role = delta ? asString(delta.role) : "";
    const turnRole = role || "assistant";
    const turn = ensureTurn(state, turnRole);
    if (delta) {
      const content = asString(delta.content);
      if (content) turn.text += content;
      const reasoning = asString(delta.reasoning_content);
      if (reasoning) turn.text += reasoning;
      for (const callEntry of asArray(delta.tool_calls)) {
        const callRecord = asRecord(callEntry);
        if (!callRecord) continue;
        const fn = asRecord(callRecord.function);
        const index = typeof callRecord.index === "number" ? callRecord.index : turn.toolCalls.length;
        let existing = turn.toolCalls.find((call) => call.index === index);
        if (!existing) {
          existing = { index, name: null, argumentsJson: null, id: asString(callRecord.id) || null };
          turn.toolCalls.push(existing);
        }
        if (fn) {
          if (asString(fn.name)) existing.name = (existing.name ?? "") + asString(fn.name);
          if (typeof fn.arguments === "string" && fn.arguments) {
            existing.argumentsJson = (existing.argumentsJson ?? "") + fn.arguments;
          }
        }
      }
    }
    if (finishReason) turn.terminalState = finishReason;
  }
  const usage = asRecord(payload.usage);
  if (usage) {
    state.usage = JSON.stringify(usage);
  }
}

// OpenAI Responses SSE: type=response.output_text.delta,
// response.function_call_arguments.delta, response.completed,
// response.failed, error, response.usage.
export function accumulateOpenAiResponsesSse(state: StreamAccumulatorState, event: SseEvent): void {
  const payload = asRecord(event.json);
  if (!payload) return;
  const type = asString(payload.type);
  if (type === "response.output_text.delta") {
    const turn = ensureTurn(state, "assistant");
    turn.text += asString(payload.delta);
  } else if (type === "response.reasoning_summary_text.delta" || type === "response.reasoning_text.delta") {
    const turn = ensureTurn(state, "assistant");
    turn.text += asString(payload.delta);
  } else if (type === "response.function_call_arguments.delta") {
    const turn = ensureTurn(state, "assistant");
    const callId = asString(payload.item_id) || asString(payload.call_id);
    let existing = turn.toolCalls.find((call) => call.id === callId) ??
      turn.toolCalls.find((call) => call.id === null);
    if (!existing) {
      existing = { index: turn.toolCalls.length, name: null, argumentsJson: null, id: callId || null };
      turn.toolCalls.push(existing);
    }
    if (typeof payload.delta === "string" && payload.delta) {
      existing.argumentsJson = (existing.argumentsJson ?? "") + payload.delta;
    }
  } else if (type === "response.completed") {
    const response = asRecord(payload.response);
    if (response) {
      const usage = asRecord(response.usage);
      if (usage) state.usage = JSON.stringify(usage);
      const status = asString(response.status);
      if (status) {
        const turn = ensureTurn(state, "assistant");
        turn.terminalState = status;
      }
    }
  } else if (type === "response.failed" || type === "error") {
    const turn = ensureTurn(state, "assistant");
    turn.terminalState = type === "error" ? "error" : "failed";
    const error = asRecord(payload.error) ?? asRecord(payload.last_error);
    if (error) {
      const message = asString(error.message);
      if (message) turn.text += `\n[error] ${message}`;
    }
  }
}

// Anthropic Messages SSE: message_start (usage), content_block_start/delta
// (text_delta, input_json_delta), content_block_stop, message_delta
// (stop_reason, usage), message_stop, error.
export function accumulateAnthropicSse(state: StreamAccumulatorState, event: SseEvent): void {
  const payload = asRecord(event.json);
  if (!payload) return;
  const type = asString(payload.type);
  if (type === "content_block_start") {
    const block = asRecord(payload.content_block);
    if (block) {
      const blockType = asString(block.type);
      if (blockType === "tool_use") {
        const turn = ensureTurn(state, "assistant");
        turn.toolCalls.push({
          index: typeof payload.index === "number" ? payload.index : turn.toolCalls.length,
          name: asString(block.name) || null,
          argumentsJson: "",
          id: asString(block.id) || null,
        });
      }
    }
  } else if (type === "content_block_delta") {
    const delta = asRecord(payload.delta);
    if (delta) {
      const deltaType = asString(delta.type);
      if (deltaType === "text_delta") {
        const turn = ensureTurn(state, "assistant");
        turn.text += asString(delta.text);
      } else if (deltaType === "input_json_delta") {
        const turn = ensureTurn(state, "assistant");
        const index = typeof payload.index === "number" ? payload.index : turn.toolCalls.length - 1;
        const existing = turn.toolCalls[index] ?? turn.toolCalls[turn.toolCalls.length - 1];
        if (existing) {
          existing.argumentsJson = (existing.argumentsJson ?? "") + asString(delta.partial_json);
        }
      }
    }
  } else if (type === "message_delta") {
    const delta = asRecord(payload.delta);
    if (delta) {
      const stopReason = asString(delta.stop_reason);
      if (stopReason) {
        const turn = ensureTurn(state, "assistant");
        turn.terminalState = stopReason;
      }
    }
    const usage = asRecord(payload.usage);
    if (usage) state.usage = JSON.stringify(usage);
  } else if (type === "error") {
    const turn = ensureTurn(state, "assistant");
    turn.terminalState = "error";
    const error = asRecord(payload.error);
    if (error) {
      const message = asString(error.message);
      if (message) turn.text += `\n[error] ${message}`;
    }
  } else if (type === "message_start") {
    const message = asRecord(payload.message);
    if (message) {
      const usage = asRecord(message.usage);
      if (usage) state.usage = JSON.stringify(usage);
    }
  }
}

// Gemini SSE: candidates[].content.parts[] with text/functionCall,
// candidates[].finishReason, usageMetadata.
export function accumulateGeminiSse(state: StreamAccumulatorState, event: SseEvent): void {
  const payload = asRecord(event.json);
  if (!payload) return;
  const candidates = asArray(payload.candidates);
  for (const candidate of candidates) {
    const candidateRecord = asRecord(candidate);
    if (!candidateRecord) continue;
    const content = asRecord(candidateRecord.content);
    if (content) {
      const role = asString(content.role) || "model";
      const turn = ensureTurn(state, role);
      for (const part of asArray(content.parts)) {
        const partRecord = asRecord(part);
        if (!partRecord) continue;
        const text = asString(partRecord.text);
        if (text) turn.text += text;
        const call = asRecord(partRecord.functionCall);
        if (call) {
          turn.toolCalls.push({
            index: turn.toolCalls.length,
            name: asString(call.name) || null,
            argumentsJson: typeof call.args === "object" ? JSON.stringify(call.args) : asString(call.args) || null,
            id: null,
          });
        }
      }
    }
    const finishReason = asString(candidateRecord.finishReason);
    if (finishReason) {
      const turn = ensureTurn(state, "model");
      turn.terminalState = finishReason;
    }
  }
  const usage = asRecord(payload.usageMetadata);
  if (usage) state.usage = JSON.stringify(usage);
}

export type ApiFamilyName = "openai" | "anthropic" | "gemini";

export function createEmptyTranscriptState(): StreamAccumulatorState {
  return { turns: [], usage: null, incomplete: false };
}

export function accumulateSseEvent(
  state: StreamAccumulatorState,
  event: SseEvent,
  apiFamily: ApiFamilyName,
  operationName: string | null,
): void {
  const operation = operationName ?? "";
  if (apiFamily === "openai") {
    if (operation.includes("responses")) {
      accumulateOpenAiResponsesSse(state, event);
    } else {
      accumulateOpenAiChatSse(state, event);
    }
  } else if (apiFamily === "anthropic") {
    accumulateAnthropicSse(state, event);
  } else {
    accumulateGeminiSse(state, event);
  }
}

export function buildTranscriptDocument(state: StreamAccumulatorState): TranscriptDocument {
  return {
    turns: state.turns.filter((turn) => turn.text.length > 0 || turn.toolCalls.length > 0 || turn.toolResults.length > 0),
    usage: state.usage,
    incomplete: state.incomplete,
  };
}
