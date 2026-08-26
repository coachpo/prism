import { describe, expect, it } from "vitest";

import { formatApiFamily } from "./apiFamilyPresentation";

describe("API-family presentation", () => {
  it("keeps the operator labels and unknown fallback", () => {
    expect(formatApiFamily("openai")).toBe("OpenAI");
    expect(formatApiFamily("anthropic")).toBe("Anthropic");
    expect(formatApiFamily("gemini")).toBe("Gemini");
    expect(formatApiFamily("other")).toBe("-");
  });
});
