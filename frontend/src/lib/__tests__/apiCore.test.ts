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

  it("does not attach X-Profile-Id to global auth settings requests", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ auth_enabled: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const { request, setApiProfileId } = await import("../api/core");
    setApiProfileId(42);

    await expect(request<{ auth_enabled: boolean }>("/api/settings/auth")).resolves.toEqual({
      auth_enabled: true,
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      headers: {
        "Content-Type": "application/json",
      },
    });
    expect((fetchMock.mock.calls[0]?.[1]?.headers as Record<string, string>)["X-Profile-Id"]).toBeUndefined();
  });

  it("does not attach X-Profile-Id to auth bootstrap requests", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ authenticated: false }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const { request, setApiProfileId } = await import("../api/core");
    setApiProfileId(42);

    await expect(request<{ authenticated: boolean }>("/api/auth/status")).resolves.toEqual({
      authenticated: false,
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect((fetchMock.mock.calls[0]?.[1]?.headers as Record<string, string>)["X-Profile-Id"]).toBeUndefined();
  });

  it("keeps X-Profile-Id on profile-scoped management requests", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify([]), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const { request, setApiProfileId } = await import("../api/core");
    setApiProfileId(42);

    await expect(request<unknown[]>("/api/vendors")).resolves.toEqual([]);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect((fetchMock.mock.calls[0]?.[1]?.headers as Record<string, string>)["X-Profile-Id"]).toBe("42");
  });
});
