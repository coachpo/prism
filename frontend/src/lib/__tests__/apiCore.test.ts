import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

describe("api core request", () => {
  it("treats successful 204 responses as undefined instead of JSON parse failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    );

    const { request } = await import("../api/core");

    await expect(request<void>("/api/vendors/6", { method: "DELETE" })).resolves.toBeUndefined();
  });
});
