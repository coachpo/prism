import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildSelfTestCurl,
  encodeGeminiModelSegment,
  type CurlBuildOutput,
} from "@/features/runtime-self-test/curlBuilder";
import {
  getEffectiveBackendOrigin,
  buildRuntimeUrl,
  resetEffectiveBackendOriginCache,
  __setApiBaseForTest,
} from "@/features/runtime-self-test/effectiveOrigin";
import { SELF_TEST_PROMPT } from "@/features/runtime-self-test/curlBuilder";

const fixtureProxyValue = "test-proxy-value";

afterEach(() => {
  __setApiBaseForTest(undefined);
  resetEffectiveBackendOriginCache();
  vi.unstubAllGlobals();
});

function jsonBody(output: CurlBuildOutput): Record<string, unknown> {
  return JSON.parse(output.body) as Record<string, unknown>;
}

function stubApiBase(value: string): void {
  __setApiBaseForTest(value);
}

describe("effective backend origin", () => {
  it("uses VITE_API_BASE when configured as an absolute origin", () => {
    stubApiBase("http://backend.local:8000/");
    expect(getEffectiveBackendOrigin().origin).toBe(
      "http://backend.local:8000",
    );
    expect(buildRuntimeUrl("/v1/chat/completions").toString()).toBe(
      "http://backend.local:8000/v1/chat/completions",
    );
    expect(
      buildRuntimeUrl(
        "/v1beta/models/gemini-2.0-flash:generateContent",
      ).toString(),
    ).toBe(
      "http://backend.local:8000/v1beta/models/gemini-2.0-flash:generateContent",
    );
  });

  it("falls back to the visible origin for same-origin deployments", () => {
    vi.stubGlobal("location", { origin: "http://localhost:5173" });
    expect(getEffectiveBackendOrigin().origin).toBe("http://localhost:5173");
    expect(buildRuntimeUrl("/v1/responses").toString()).toBe(
      "http://localhost:5173/v1/responses",
    );
  });

  it("rejects configured bases with credentials, query, fragment or path", () => {
    stubApiBase("http://user:pass@backend.local:8000/");
    expect(() => getEffectiveBackendOrigin()).toThrow();
    stubApiBase("http://backend.local:8000/api");
    expect(() => getEffectiveBackendOrigin()).toThrow();
    stubApiBase("http://backend.local:8000/?q=1");
    expect(() => getEffectiveBackendOrigin()).toThrow();
  });

  it("never derives runtime URLs from the dashboard origin when standalone backend is set", () => {
    stubApiBase("http://backend.local:8000/");
    vi.stubGlobal("location", { origin: "http://dashboard.local:5173" });
    expect(buildRuntimeUrl("/v1/messages").toString()).toBe(
      "http://backend.local:8000/v1/messages",
    );
  });
});

describe("operation-aware curl builder", () => {
  it("builds OpenAI Chat Completions curl for chat_completions_only", () => {
    const output = buildSelfTestCurl({
      apiFamily: "openai",
      openaiAcceptedFormat: "chat_completions_only",
      modelId: "gpt-5.6-luna",
      proxyKey: fixtureProxyValue,
    });
    expect(output.operation).toBe("chat_completions");
    expect(output.url).toContain("/v1/chat/completions");
    expect(output.headers.Authorization).toBe(`Bearer ${fixtureProxyValue}`);
    const body = jsonBody(output);
    expect(body.model).toBe("gpt-5.6-luna");
    expect(body.stream).toBe(false);
    expect(body.max_completion_tokens).toBe(8);
    expect((body.messages as Array<{ content: string }>)[0].content).toBe(
      SELF_TEST_PROMPT,
    );
    expect(output.curl).toContain("curl POST");
    expect(output.curl).toContain(
      "-H 'Authorization: Bearer " + fixtureProxyValue + "'",
    );
    expect(output.curl).toContain(`-d '${output.body}'`);
  });

  it("builds OpenAI Responses curl for responses_only", () => {
    const output = buildSelfTestCurl({
      apiFamily: "openai",
      openaiAcceptedFormat: "responses_only",
      modelId: "gpt-5.6-luna",
      proxyKey: fixtureProxyValue,
    });
    expect(output.operation).toBe("responses");
    expect(output.url).toContain("/v1/responses");
    const body = jsonBody(output);
    expect(body.input).toBe(SELF_TEST_PROMPT);
    expect(body.max_output_tokens).toBe(8);
    expect(body.stream).toBe(false);
  });

  it("defaults dual_native to Responses and allows explicit Chat switch", () => {
    const dual = buildSelfTestCurl({
      apiFamily: "openai",
      openaiAcceptedFormat: "dual_native",
      modelId: "gpt-5.6-luna",
      proxyKey: fixtureProxyValue,
    });
    expect(dual.operation).toBe("responses");
    expect(dual.url).toContain("/v1/responses");

    const chat = buildSelfTestCurl({
      apiFamily: "openai",
      openaiAcceptedFormat: "dual_native",
      modelId: "gpt-5.6-luna",
      proxyKey: fixtureProxyValue,
      openaiOperation: "chat_completions",
    });
    expect(chat.operation).toBe("chat_completions");
    expect(chat.url).toContain("/v1/chat/completions");
  });

  it("builds Anthropic Messages curl with family headers", () => {
    const output = buildSelfTestCurl({
      apiFamily: "anthropic",
      openaiAcceptedFormat: null,
      modelId: "claude-sonnet-4-5",
      proxyKey: fixtureProxyValue,
    });
    expect(output.operation).toBe("messages");
    expect(output.url).toContain("/v1/messages");
    expect(output.headers["X-API-Key"]).toBe(fixtureProxyValue);
    expect(output.headers["anthropic-version"]).toBe("2023-06-01");
    const body = jsonBody(output);
    expect(body.model).toBe("claude-sonnet-4-5");
    expect(body.max_tokens).toBe(8);
    expect(body.stream).toBe(false);
  });

  it("builds Gemini generateContent curl with encoded model path segment", () => {
    const output = buildSelfTestCurl({
      apiFamily: "gemini",
      openaiAcceptedFormat: null,
      modelId: "gemini-2.5-pro:thinking",
      proxyKey: fixtureProxyValue,
    });
    expect(output.operation).toBe("generate_content");
    expect(output.url).toContain(
      `/v1beta/models/${encodeGeminiModelSegment("gemini-2.5-pro:thinking")}:generateContent`,
    );
    expect(output.headers["X-Goog-Api-Key"]).toBe(fixtureProxyValue);
    const body = jsonBody(output);
    expect(
      (body.generationConfig as { maxOutputTokens: number }).maxOutputTokens,
    ).toBe(8);
    expect(body.model).toBeUndefined();
  });

  it("exposes gateway origin and family route base", () => {
    stubApiBase("http://backend.local:8000/");
    const output = buildSelfTestCurl({
      apiFamily: "openai",
      openaiAcceptedFormat: "responses_only",
      modelId: "gpt-5.6-luna",
      proxyKey: fixtureProxyValue,
    });
    expect(output.gatewayOrigin).toBe("http://backend.local:8000");
    expect(output.familyBaseUrl).toBe("http://backend.local:8000/v1");
    const gemini = buildSelfTestCurl({
      apiFamily: "gemini",
      openaiAcceptedFormat: null,
      modelId: "gemini-2.5-flash",
      proxyKey: fixtureProxyValue,
    });
    expect(gemini.familyBaseUrl).toBe("http://backend.local:8000/v1beta");
  });

  it("escapes POSIX shell metacharacters in model ids and secrets", () => {
    const trickyModel = "model with 'quotes' & $(danger) ; spaces";
    const output = buildSelfTestCurl({
      apiFamily: "anthropic",
      openaiAcceptedFormat: null,
      modelId: trickyModel,
      proxyKey: fixtureProxyValue,
    });
    const body = jsonBody(output);
    expect(body.model).toBe(trickyModel);
    // Single-quoted shell escaping: every ' becomes '\'' (inside the JSON body).
    expect(output.curl).toContain("quotes'\\''");
    expect(output.curl).toContain("$(danger)");
  });

  it("keeps the secret out of the URL, query and body for every family", () => {
    const inputs = [
      {
        apiFamily: "openai",
        openaiAcceptedFormat: "responses_only",
        modelId: "m1",
      },
      {
        apiFamily: "openai",
        openaiAcceptedFormat: "chat_completions_only",
        modelId: "m2",
      },
      { apiFamily: "anthropic", openaiAcceptedFormat: null, modelId: "m3" },
      { apiFamily: "gemini", openaiAcceptedFormat: null, modelId: "m4" },
    ] as const;
    for (const input of inputs) {
      const output = buildSelfTestCurl({
        ...input,
        proxyKey: fixtureProxyValue,
      });
      expect(output.url).not.toContain(fixtureProxyValue);
      expect(output.body).not.toContain(fixtureProxyValue);
    }
  });
});
