import { describe, expect, it } from "vitest";
import { zhCNMessages } from "@/i18n/messages/zh-CN";
import { routeExplanationOperations } from "./routeExplanationOperations";

describe("route explanation operation choices", () => {
  it("covers all eleven existing model-bound native operations with readable labels", () => {
    const options = ["openai", "anthropic", "gemini"].flatMap(family => routeExplanationOperations(family, zhCNMessages));
    expect(options.map(([name]) => name)).toEqual([
      "openai.chat_completions", "openai.responses", "openai.responses.input_tokens", "openai.responses.compact",
      "openai.images.generations", "openai.images.edits", "anthropic.messages", "anthropic.count_tokens",
      "gemini.generate_content", "gemini.stream_generate_content", "gemini.count_tokens",
    ]);
    for (const [name, label] of options) {
      expect(label).not.toBe(name);
      expect(label).not.toMatch(/^(openai|anthropic|gemini)\./);
    }
    expect(routeExplanationOperations("unknown", zhCNMessages)).toEqual([]);
  });
});
