import { describe, expect, it } from "vitest";
import {
  dropSensitiveDeep,
  extractClientConfig,
  keyLooksSensitive,
} from "./clientConfigExtract";

describe("keyLooksSensitive", () => {
  it.each([
    "apiKey",
    "api_key",
    "Authorization",
    "x-api-key",
    "client_secret",
    "privateKey",
    "accessToken",
    "sessionToken",
    "refresh-token",
    "proxyKey",
    "proxy_key",
    "Proxy-Key",
    "x-auth-token",
    "access-token",
    "session-token",
    "x-jwt-assertion",
    "x-trace-id",
    "x-upstream-trace",
  ])("flags %s", (key) => {
    expect(keyLooksSensitive(key)).toBe(true);
  });

  it.each([
    "headers",
    "baseURL",
    "thinkingLevelMap",
    "limit",
    "maxTokens",
    "x-trace",
    "access-control-allow-credentials",
  ])(
    "passes %s",
    (key) => {
      expect(keyLooksSensitive(key)).toBe(false);
    },
  );
});

describe("dropSensitiveDeep", () => {
  it("strips credential keys at every depth and preserves explicit zeros", () => {
    const cleaned = dropSensitiveDeep({
      name: "m",
      maxTokens: 32768,
      apiKey: "sk-attack",
      nested: { authorization: "Bearer x", keep: 0 },
      list: [
        {
          token: "t",
          accessToken: "access",
          sessionToken: "session",
          ok: false,
        },
      ],
    }) as Record<string, unknown>;
    expect(cleaned).toEqual({
      name: "m",
      maxTokens: 32768,
      nested: { keep: 0 },
      list: [{ ok: false }],
    });
  });
});

describe("extractClientConfig", () => {
  it("parses Pi models.json, drops provider apiKey, and extracts model fields", () => {
    const result = extractClientConfig(
      JSON.stringify({
        providers: {
          "my-proxy": {
            baseUrl: "https://x/v1",
            apiKey: "$SECRET_KEY",
            api: "openai-completions",
            models: [
              {
                id: "glm-5.2",
                name: "GLM",
                reasoning: true,
                contextWindow: 200000,
                maxTokens: 32768,
                thinkingLevelMap: { high: "high" },
                compat: { supportsDeveloperRole: false },
              },
            ],
          },
        },
      }),
    );
    expect(result.sourceKind).toBe("pi-models");
    expect(result.models).toHaveLength(1);
    expect(result.models[0].clientId).toBe("my-proxy/glm-5.2");
    expect(result.models[0].modelId).toBe("glm-5.2");
    expect(result.models[0].fields).toEqual({
      thinkingLevelMap: { high: "high" },
      compat: { supportsDeveloperRole: false },
    });
    expect(JSON.stringify(result.models)).not.toContain("SECRET");
    expect(result.notes.some((note) => note.includes("apiKey"))).toBe(true);
  });

  it("parses Pi models-store.json catalog shape", () => {
    const result = extractClientConfig(
      JSON.stringify({
        "my-provider": {
          models: {
            "m-1": {
              id: "m-1",
              name: "M One",
              compat: { supportsDeveloperRole: true },
            },
          },
          fetchedAt: 1234,
        },
      }),
    );
    expect(result.sourceKind).toBe("pi-store");
    expect(result.models[0].fields).toEqual({
      compat: { supportsDeveloperRole: true },
    });
    expect(result.models[0].modelId).toBe("m-1");
  });

  it("parses OpenCode JSONC with comments, drops options.apiKey, and queues safe headers", () => {
    const jsonc = `{
      // my opencode config
      "$schema": "https://opencode.ai/config.json",
      "provider": {
        "relay": {
          "options": {
            "baseURL": "https://relay/v1",
            "apiKey": "{env:TOKEN}",
            "headers": { "x-provider-options": "must-not-inherit" },
          },
          "headers": {
            "x-trace": "abc",
            "authorization": "Bearer zzz",
            "accessToken": "secret",
          },
          "models": {
            "vendor/gpt-x": {
              "name": "GPT X",
              "reasoning": true,
              "variants": { "high": { "reasoningEffort": "high" } },
              "headers": { "x-model": "confirmed-later" },
              "options": {
                "reasoningEffort": "medium",
                "baseURL": "https://must-not-override.example/v1",
                "apiKey": "model-secret",
                "headers": { "x-model-options": "confirmed-too" },
              },
            },
          },
        },
      },
    }`;
    const result = extractClientConfig(jsonc);
    expect(result.sourceKind).toBe("opencode");
    expect(result.models[0].clientId).toBe("relay/vendor/gpt-x");
    expect(result.models[0].modelId).toBe("vendor/gpt-x");
    expect(result.models[0].fields.variants).toEqual({
      high: { reasoningEffort: "high" },
    });
    expect(result.models[0].fields.options).toEqual({
      reasoningEffort: "medium",
    });
    expect(result.models[0].fields).not.toHaveProperty("name");
    expect(result.models[0].fields).not.toHaveProperty("reasoning");
    // Sensitive header dropped immediately; safe header awaits confirmation.
    expect(result.headerCandidates.map((header) => header.name).sort()).toEqual(
      ["x-model", "x-model-options", "x-trace"],
    );
    expect(
      result.headerCandidates.find((header) => header.name === "x-model")
        ?.modelId,
    ).toBe("vendor/gpt-x");
    expect(JSON.stringify(result)).not.toContain("Bearer zzz");
    expect(JSON.stringify(result)).not.toContain("secret");
    expect(JSON.stringify(result)).not.toContain("{env:TOKEN}");
    expect(JSON.stringify(result)).not.toContain("must-not-inherit");
    expect(JSON.stringify(result)).not.toContain("model-secret");
    expect(JSON.stringify(result)).not.toContain("must-not-override");
  });

  it("rejects payloads that match no supported format", () => {
    expect(() => extractClientConfig('{"random": true}')).toThrow();
    expect(() => extractClientConfig("not json")).toThrow();
  });
});
