import { describe, expect, it, vi } from "vitest";

describe("reference data registry", () => {
  it("deduplicates in-flight loads for the same dataset revision", async () => {
    const { createReferenceDataStore } = await import("../referenceDataRegistry");
    const loadModels = vi.fn().mockResolvedValue(["gpt-4o-mini"]);
    const store = createReferenceDataStore({
      models: { load: loadModels },
    });

    const [first, second] = await Promise.all([
      store.get("models", 1),
      store.get("models", 1),
    ]);

    expect(first).toEqual(["gpt-4o-mini"]);
    expect(second).toEqual(["gpt-4o-mini"]);
    expect(loadModels).toHaveBeenCalledTimes(1);
  });

  it("prunes stale revisions for one dataset without invalidating other datasets", async () => {
    const { createReferenceDataStore } = await import("../referenceDataRegistry");
    const loadModels = vi
      .fn<() => Promise<string[]>>()
      .mockResolvedValueOnce(["revision-1"])
      .mockResolvedValueOnce(["revision-2"])
      .mockResolvedValueOnce(["revision-1-reloaded"]);
    const loadProviders = vi.fn().mockResolvedValue(["openai"]);
    const store = createReferenceDataStore({
      models: { load: loadModels },
      providers: { load: loadProviders },
    });

    await store.get("models", 1);
    await store.get("providers", 1);
    await store.get("models", 2);

    expect(await store.get("models", 1)).toEqual(["revision-1-reloaded"]);
    expect(await store.get("providers", 1)).toEqual(["openai"]);
    expect(loadModels).toHaveBeenCalledTimes(3);
    expect(loadProviders).toHaveBeenCalledTimes(1);
  });
});
