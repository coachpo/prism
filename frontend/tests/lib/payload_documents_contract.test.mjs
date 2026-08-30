import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { buildPayloadViewModel } = load(
  path.join(frontendDir, "src/pages/request-logs/detail/payloadDocumentViewModel.ts"),
);

test("payload view model exposes three views for OpenAI Chat stream and reassembles text + tool calls", () => {
  const sse = [
    'data: {"choices":[{"delta":{"role":"assistant","content":"Hello"}}]}',
    "",
    'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":"{\\"city\\":\\"Paris\\"}"}}]}}]}',
    "",
    'data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}',
    "",
    'data: {"choices":[{"delta":{},"finish_reason":null}]}',
    "",
    "data: [DONE]",
    "",
  ].join("\n");

  const model = buildPayloadViewModel(sse, "openai", "response", "openai.chat_completions");
  assert.equal(model.isStreaming, true);
  assert.equal(model.availability.length, 3);
  assert.deepEqual(model.availability.map((view) => view.kind), ["transcript", "json_events", "raw_sse"]);
  assert.ok(model.transcript !== null);
  assert.ok(model.transcript.turns.length >= 1);
  const assistantTurn = model.transcript.turns.find((turn) => turn.role === "assistant");
  assert.ok(assistantTurn, "expected an assistant turn");
  assert.equal(assistantTurn.text, "Hello");
  assert.equal(assistantTurn.toolCalls.length, 1);
  assert.equal(assistantTurn.toolCalls[0].name, "get_weather");
  assert.equal(assistantTurn.toolCalls[0].argumentsJson, '{"city":"Paris"}');
  assert.equal(model.sseEvents?.length, 5);
});

test("payload view model exposes three views for Anthropic stream with tool_use", () => {
  const sse = [
    'event: content_block_start',
    'data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"search"}}',
    "",
    'event: content_block_delta',
    'data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\\"q\\":\\"prism\\"}"}}',
    "",
    'event: content_block_stop',
    'data: {"type":"content_block_stop","index":0}',
    "",
  ].join("\n");

  const model = buildPayloadViewModel(sse, "anthropic", "response", "anthropic.messages");
  assert.equal(model.isStreaming, true);
  assert.ok(model.transcript !== null);
  const assistantTurn = model.transcript.turns.find((turn) => turn.role === "assistant");
  assert.ok(assistantTurn, "expected an assistant turn");
  assert.equal(assistantTurn.toolCalls.length, 1);
  assert.equal(assistantTurn.toolCalls[0].name, "search");
  assert.equal(assistantTurn.toolCalls[0].argumentsJson, '{"q":"prism"}');
});

test("payload view model exposes message + JSON views for non-stream tool bodies", () => {
  const body = JSON.stringify({
    model: "gpt-5.6",
    messages: [
      { role: "user", content: "weather?" },
      {
        role: "assistant",
        content: null,
        tool_calls: [{ id: "call_1", type: "function", function: { name: "get_weather", arguments: '{"city":"Paris"}' } }],
      },
      { role: "tool", tool_call_id: "call_1", content: "{\"temp\":22}" },
    ],
  });

  const model = buildPayloadViewModel(body, "openai", "request", "openai.chat_completions");
  assert.equal(model.isStreaming, false);
  assert.deepEqual(model.availability.map((view) => view.kind), ["transcript", "json"]);
  assert.ok(model.transcript !== null);
  const toolTurn = model.transcript.turns[0];
  assert.equal(toolTurn.toolCalls.length, 1);
  assert.equal(toolTurn.toolCalls[0].name, "get_weather");
  assert.equal(toolTurn.toolResults.length, 1);
  assert.equal(toolTurn.toolResults[0].content, '{"temp":22}');
});

test("payload view model treats binary/invalid UTF-8 bodies as unparseable", () => {
  const binary = "\uFFFD\uFFFD\u0000\u0001binary";
  const model = buildPayloadViewModel(binary, "openai", "response", "openai.responses");
  assert.equal(model.readableText, false);
  assert.deepEqual(model.availability.map((view) => view.kind), ["unparseable"]);
  assert.equal(model.transcript, null);
});
