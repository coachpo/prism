// Source-linked catalog pricing import: the shared components the /route/pricing
// entry and the model-detail action both mount. These cover the discovery rules
// (unique match advances, zero/multi never auto-picks), the full five-component
// preview with explicit zero kept distinct from absent, the commit gates, and
// the target-set invalidation that forces a fresh preview.
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { models as modelsApi } from "@/lib/api/models";
import { pricingTemplates } from "@/lib/api/pricingTemplates";
import {
  getSharedConnectionOptions,
  getSharedModels,
} from "@/lib/referenceData";
import type {
  CatalogPricingCommitResponse,
  CatalogPricingPreviewResponse,
  ConnectionDropdownItem,
  ModelCatalogMatchPreviewResponse,
  ModelConfigListItem,
} from "@/lib/types";
import type { CatalogPricingSource } from "./useCatalogPricingImport";

// The preview panel renders the catalog fetch stamp through the operator
// timezone, which otherwise reads live settings over the network.
vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    timezone: "UTC",
    format: (iso: string) => iso,
    loading: false,
    refresh: async () => "UTC",
  }),
}));

vi.mock("@/lib/api/models", () => ({
  models: {
    catalog: {
      matchPreview: vi.fn(),
      candidates: vi.fn(),
    },
  },
}));

vi.mock("@/lib/api/pricingTemplates", () => ({
  pricingTemplates: {
    catalogPreview: vi.fn(),
    catalogCommit: vi.fn(),
  },
}));

vi.mock("@/lib/referenceData", () => ({
  getSharedConnectionOptions: vi.fn(),
  getSharedModels: vi.fn(),
}));

const matchPreviewMock = vi.mocked(modelsApi.catalog.matchPreview);
const candidatesMock = vi.mocked(modelsApi.catalog.candidates);
const catalogPreviewMock = vi.mocked(pricingTemplates.catalogPreview);
const catalogCommitMock = vi.mocked(pricingTemplates.catalogCommit);
const getSharedModelsMock = vi.mocked(getSharedModels);
const getSharedConnectionOptionsMock = vi.mocked(getSharedConnectionOptions);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, reject, resolve };
}

// jsdom does not implement the pointer-capture API Radix Select drives, and the
// list viewport needs scrollIntoView. Call history is cleared per test so a
// shared module mock can never make one case look like another's.
beforeEach(() => {
  vi.clearAllMocks();
  getSharedModelsMock.mockResolvedValue([]);
  getSharedConnectionOptionsMock.mockResolvedValue([]);
  Object.defineProperties(HTMLElement.prototype, {
    hasPointerCapture: { configurable: true, value: vi.fn(() => false) },
    releasePointerCapture: { configurable: true, value: vi.fn() },
    setPointerCapture: { configurable: true, value: vi.fn() },
    scrollIntoView: { configurable: true, value: vi.fn() },
  });
});

function prismModel(
  overrides?: Partial<ModelConfigListItem>,
): ModelConfigListItem {
  return {
    id: 7,
    profile_id: 1,
    api_family: "openai",
    model_id: "gpt-five-part",
    display_name: "GPT Five Part",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: true,
    loadbalance_strategy_id: 11,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    incoming_model_target_count: 0,
    configuration_warnings: [],
    created_at: "2026-08-25T12:00:00Z",
    updated_at: "2026-08-25T12:00:00Z",
    ...overrides,
  } as ModelConfigListItem;
}

function previewResponse(
  overrides?: Partial<CatalogPricingPreviewResponse>,
): CatalogPricingPreviewResponse {
  return {
    schema_version: 1,
    offering: {
      provider_id: "openai",
      catalog_model_id: "gpt-five-part",
      name: "GPT Five Part",
    },
    model: {
      model_config_id: 7,
      model_id: "gpt-five-part",
      display_name: "GPT Five Part",
      api_family: "openai",
    },
    catalog_revision: '"catalog-rev-9"',
    fetched_at: "2026-08-25T12:00:00Z",
    plan: {
      template_kind: "standard",
      cards: {
        standard: {
          input_price: "1.25",
          output_price: "10",
          cached_input_price: "0",
          cache_creation_price: "1.5",
          reasoning_price: "12.5",
        },
      },
      incompatibilities: [],
    },
    action: "create",
    drift: false,
    committable: true,
    preview_hash: "hash-abc",
    targets: [],
    reporting_currency_code: "USD",
    catalog_currency: "USD",
    pricing_unit: "PER_1M",
    ...overrides,
  } as CatalogPricingPreviewResponse;
}

function uniqueMatchResponse(
  providerId: string,
  catalogModelId: string,
): ModelCatalogMatchPreviewResponse {
  return {
    committable: true,
    provider_id: providerId,
    catalog_model_id: catalogModelId,
    candidates: [
      {
        provider_id: providerId,
        provider_name: providerId,
        model_id: catalogModelId,
        name: catalogModelId,
      },
    ],
    reason: "unique_match",
    catalog_revision: '"catalog-rev-9"',
    fetched_at: "2026-08-25T12:00:00Z",
  };
}

function resetHappyPath() {
  matchPreviewMock.mockResolvedValue({
    committable: true,
    provider_id: "openai",
    catalog_model_id: "gpt-five-part",
    candidates: [
      {
        provider_id: "openai",
        provider_name: "OpenAI",
        model_id: "gpt-five-part",
        name: "GPT Five Part",
      },
    ],
    reason: "unique_match",
    catalog_revision: '"catalog-rev-9"',
    fetched_at: "2026-08-25T12:00:00Z",
  });
  candidatesMock.mockResolvedValue({
    items: [],
    total: 0,
    limit: 20,
    offset: 0,
    scope: "family",
    catalog_revision: '"catalog-rev-9"',
    fetched_at: "2026-08-25T12:00:00Z",
  });
  catalogPreviewMock.mockResolvedValue(previewResponse());
  catalogCommitMock.mockResolvedValue({
    created: true,
    updated: false,
    assigned_connection_ids: [],
    template_id: 41,
    template_name: "openai/gpt-five-part",
    revision_id: 77,
    version: 1,
    drift_confirmed: false,
  });
}

async function renderDiscovery(models = [prismModel()]) {
  const { CatalogOfferingDiscovery } = await import(
    "@/features/pricing/catalog/CatalogOfferingDiscovery"
  );
  const onResolved = vi.fn();
  render(
    <LocaleProvider>
      <CatalogOfferingDiscovery models={models} onResolved={onResolved} />
    </LocaleProvider>,
  );
  return { onResolved };
}

describe("catalog offering discovery", () => {
  it("advances a unique exact match straight into a coordinates source", async () => {
    resetHappyPath();
    const { onResolved } = await renderDiscovery();

    const trigger = screen.getByRole("combobox", { name: /Prism 模型/ });
    await userEvent.click(trigger);
    await userEvent.click(
      await screen.findByText(/GPT Five Part · gpt-five-part/),
    );
    expect(trigger).toHaveTextContent("OpenAI");

    await waitFor(() =>
      expect(onResolved).toHaveBeenCalledWith({
        kind: "coordinates",
        providerId: "openai",
        catalogModelId: "gpt-five-part",
        modelConfigId: 7,
      }),
    );
    expect(await screen.findByText("发现唯一精确匹配")).toBeInTheDocument();
    // Discovery never previews or commits on its own.
    expect(catalogPreviewMock).not.toHaveBeenCalled();
    expect(catalogCommitMock).not.toHaveBeenCalled();
  });

  it("drops a late exact-match response after another model is selected", async () => {
    resetHappyPath();
    const firstMatch = deferred<ModelCatalogMatchPreviewResponse>();
    matchPreviewMock.mockImplementation((modelConfigId) =>
      modelConfigId === 7
        ? firstMatch.promise
        : Promise.resolve(uniqueMatchResponse("provider-b", "model-b")),
    );
    const { onResolved } = await renderDiscovery([
      prismModel({ display_name: "Model A", model_id: "model-a" }),
      prismModel({ id: 8, display_name: "Model B", model_id: "model-b" }),
    ]);

    const trigger = screen.getByRole("combobox", { name: /Prism 模型/ });
    await userEvent.click(trigger);
    await userEvent.click(await screen.findByText(/Model A · model-a/));
    await waitFor(() => expect(matchPreviewMock).toHaveBeenCalledWith(7));

    await userEvent.click(trigger);
    await userEvent.click(await screen.findByText(/Model B · model-b/));
    await waitFor(() =>
      expect(onResolved).toHaveBeenCalledWith({
        kind: "coordinates",
        providerId: "provider-b",
        catalogModelId: "model-b",
        modelConfigId: 8,
      }),
    );

    await act(async () => {
      firstMatch.resolve(uniqueMatchResponse("provider-a", "model-a"));
      await firstMatch.promise;
    });
    const resolved = onResolved.mock.calls
      .map(([source]) => source)
      .filter((source) => source !== null);
    expect(resolved.at(-1)).toMatchObject({
      providerId: "provider-b",
      catalogModelId: "model-b",
      modelConfigId: 8,
    });
    expect(screen.getByText("provider-b/model-b")).toBeInTheDocument();
    expect(screen.queryByText("provider-a/model-a")).not.toBeInTheDocument();
  });

  it("separates a candidate read failure from an empty result and retries", async () => {
    resetHappyPath();
    matchPreviewMock.mockResolvedValue({
      committable: false,
      candidates: [],
      reason: "no_match",
      catalog_revision: '"catalog-rev-9"',
      fetched_at: "2026-08-25T12:00:00Z",
    });
    candidatesMock
      .mockRejectedValueOnce(new Error("candidate_down"))
      .mockResolvedValue({
        items: [
          {
            provider_id: "codex",
            provider_name: "Codex",
            model_id: "gpt-5.6-luna",
            name: "GPT 5.6 Luna",
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
        scope: "family",
        catalog_revision: '"catalog-rev-9"',
        fetched_at: "2026-08-25T12:00:00Z",
      });
    await renderDiscovery();

    const trigger = screen.getByRole("combobox", { name: /Prism 模型/ });
    await userEvent.click(trigger);
    await userEvent.click(await screen.findByText(/gpt-five-part/));

    expect(await screen.findByText("候选条目读取失败")).toBeInTheDocument();
    expect(screen.getByText("candidate_down")).toBeInTheDocument();
    expect(
      screen.queryByText("没有匹配的候选条目，请调整关键词。"),
    ).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "重新读取候选" }));
    expect(
      await screen.findByRole("option", { name: /codex\/gpt-5\.6-luna/ }),
    ).toBeInTheDocument();
  });
});

describe("catalog import reference data", () => {
  it("keeps the newest model and target read when an older load resolves late", async () => {
    const firstModels = deferred<ModelConfigListItem[]>();
    const firstTargets = deferred<ConnectionDropdownItem[]>();
    const latestModel = prismModel({
      id: 8,
      display_name: "Latest Model",
      model_id: "latest-model",
    });
    const latestTarget = {
      id: 12,
      name: "Latest Target",
    } as ConnectionDropdownItem;
    getSharedModelsMock
      .mockReturnValueOnce(firstModels.promise)
      .mockResolvedValueOnce([latestModel]);
    getSharedConnectionOptionsMock
      .mockReturnValueOnce(firstTargets.promise)
      .mockResolvedValueOnce([latestTarget]);
    const { useCatalogImportReferenceData } = await import(
      "@/features/pricing/catalog/useCatalogImportReferenceData"
    );

    const { result } = renderHook(
      () => useCatalogImportReferenceData(0, true),
      { wrapper: LocaleProvider },
    );
    await waitFor(() => expect(result.current.loading).toBe(true));
    act(() => {
      void result.current.refresh();
    });
    await waitFor(() =>
      expect(result.current.models.map((model) => model.id)).toEqual([8]),
    );
    expect(result.current.targets.map((target) => target.id)).toEqual([12]);

    await act(async () => {
      firstModels.resolve([prismModel()]);
      firstTargets.resolve([
        { id: 11, name: "Stale Target" } as ConnectionDropdownItem,
      ]);
      await Promise.all([firstModels.promise, firstTargets.promise]);
    });
    expect(result.current.models.map((model) => model.id)).toEqual([8]);
    expect(result.current.targets.map((target) => target.id)).toEqual([12]);
  });
});

describe("catalog pricing preview display", () => {
  async function renderPreview(preview: CatalogPricingPreviewResponse) {
    const { CatalogPricingPreviewPanel } = await import(
      "@/features/pricing/catalog/CatalogPricingPreviewPanel"
    );
    return render(
      <LocaleProvider>
        <CatalogPricingPreviewPanel preview={preview} />
      </LocaleProvider>,
    );
  }

  it("shows all five components, keeping an explicit zero distinct from absent", async () => {
    await renderPreview(previewResponse());

    const table = await screen.findByTestId("catalog-pricing-cards");
    const headers = within(table)
      .getAllByRole("columnheader")
      .map((node) => node.textContent);
    expect(headers).toEqual(
      expect.arrayContaining([
        "角色",
        "输入",
        "输出",
        "缓存读",
        "缓存写",
        "推理",
      ]),
    );

    const cells = within(table)
      .getAllByRole("cell")
      .map((node) => node.textContent);
    expect(cells).toEqual(
      expect.arrayContaining(["标准价", "1.25", "10", "0", "1.5", "12.5"]),
    );
    // The zero is a configured free price, never the missing marker.
    expect(cells).not.toContain("—");
  });

  it("renders absent specialty components as missing, not zero", async () => {
    await renderPreview(
      previewResponse({
        plan: {
          template_kind: "standard",
          cards: {
            standard: {
              input_price: "3",
              output_price: "15",
              cached_input_price: null,
              cache_creation_price: null,
              reasoning_price: null,
            },
          },
          incompatibilities: [],
        },
      }),
    );
    const table = await screen.findByTestId("catalog-pricing-cards");
    const cells = within(table)
      .getAllByRole("cell")
      .map((node) => node.textContent);
    expect(cells.filter((value) => value?.startsWith("—"))).toHaveLength(3);
    expect(table.querySelectorAll('[data-slot="missing-value"]')).toHaveLength(
      3,
    );
  });

  it("names both ends of the mapping plus revision, fetch stamp and unit", async () => {
    await renderPreview(previewResponse());
    // The offering display name doubles as the panel heading and the Prism
    // model label, so both occurrences are expected.
    expect(
      (await screen.findAllByText("GPT Five Part")).length,
    ).toBeGreaterThan(0);
    expect(screen.getAllByText("openai/gpt-five-part").length).toBeGreaterThan(
      0,
    );
    expect(screen.getByText("gpt-five-part")).toBeInTheDocument();
    expect(screen.getByText('"catalog-rev-9"')).toBeInTheDocument();
    // The unit appears both in the currency row and in the explicit note.
    expect(
      screen.getAllByText(/USD\/PER_1M|· USD\/PER_1M/).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/按原值写入，不做汇率换算/)).toBeInTheDocument();
    expect(screen.getByText("2026-08-25T12:00:00Z")).toBeInTheDocument();
  });

  it("surfaces a tier threshold verbatim for a tiered plan", async () => {
    await renderPreview(
      previewResponse({
        plan: {
          template_kind: "tiered",
          cards: {
            tier_base: {
              input_price: "30",
              output_price: "180",
              cached_input_price: null,
              cache_creation_price: null,
              reasoning_price: null,
            },
            tier_above: {
              input_price: "60",
              output_price: "270",
              cached_input_price: null,
              cache_creation_price: null,
              reasoning_price: null,
            },
          },
          tier_input_tokens_above: 272000,
          incompatibilities: [],
        },
      }),
    );
    expect(
      await screen.findByText(/输入超过 272000 令牌/),
    ).toBeInTheDocument();
  });

  it("labels stable incompatibility reasons instead of leaking raw enums", async () => {
    await renderPreview(
      previewResponse({
        committable: false,
        preview_hash: undefined as never,
        plan: {
          template_kind: "standard",
          cards: {},
          incompatibilities: [
            { field: "cost.input_audio", reason: "audio_cost_present" },
            { field: "cost.tiers", reason: "multiple_tiers" },
          ],
        },
      }),
    );
    expect(await screen.findByText("价格不可导入")).toBeInTheDocument();
    expect(
      screen.getByText("目录条目含音频计价，Prism 无对应价格种类"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("目录条目含多个阶梯，无法映射为单一阈值"),
    ).toBeInTheDocument();
    // The field path stays visible as evidence, the reason code does not.
    expect(screen.getByText("cost.input_audio")).toBeInTheDocument();
    expect(screen.queryByText("audio_cost_present")).not.toBeInTheDocument();
  });
});

describe("catalog pricing dialog commit gates", () => {
  async function renderDialog(options?: {
    initialConnectionIds?: number[];
    lockedConnectionIds?: number[];
    source?: CatalogPricingSource;
  }) {
    const { CatalogPricingDialog } = await import(
      "@/features/pricing/catalog/CatalogPricingDialog"
    );
    const onCommitted = vi.fn();
    const onClose = vi.fn();
    const renderNode = (source: CatalogPricingSource) => (
      <LocaleProvider>
        <CatalogPricingDialog
          isOpen
          source={source}
          title="从目录生成价格"
          targets={[
            { id: 11, name: "目标 A" },
            { id: 12, name: "目标 B" },
          ]}
          initialConnectionIds={options?.initialConnectionIds ?? []}
          lockedConnectionIds={options?.lockedConnectionIds ?? []}
          onClose={onClose}
          onCommitted={onCommitted}
        />
      </LocaleProvider>
    );
    const utils = render(
      renderNode(options?.source ?? { kind: "bound_model", modelConfigId: 7 }),
    );
    return {
      ...utils,
      onClose,
      onCommitted,
      rerenderSource: (source: CatalogPricingSource) =>
        utils.rerender(renderNode(source)),
    };
  }

  it("previews with no target by default and commits a template-only import", async () => {
    resetHappyPath();
    catalogCommitMock.mockResolvedValue({
      created: true,
      updated: false,
      assigned_connection_ids: [],
      template_id: 42,
      template_name: "openai/gpt-five-part (2)",
      revision_id: 78,
      version: 1,
      drift_confirmed: false,
    });
    const { onCommitted } = await renderDialog();

    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(catalogPreviewMock).toHaveBeenCalled());
    expect(catalogPreviewMock.mock.calls[0][0]).toMatchObject({
      model_config_id: 7,
      connection_ids: [],
    });

    // No target is preselected on the pricing surface, and committing is still
    // allowed: it creates the template and assigns nothing.
    expect(submit).not.toBeDisabled();
    expect(submit).toHaveTextContent("生成或刷新模板");
    await userEvent.click(submit);
    await waitFor(() => expect(catalogCommitMock).toHaveBeenCalled());
    expect(catalogCommitMock.mock.calls[0][0]).toMatchObject({
      connection_ids: [],
    });
    expect(onCommitted).toHaveBeenCalledWith("openai/gpt-five-part (2)", 0);
  });

  it("re-previews whenever the target set changes", async () => {
    resetHappyPath();
    await renderDialog();
    await screen.findByTestId("catalog-pricing-cards");
    expect(catalogPreviewMock).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByTestId("catalog-pricing-target-11"));

    await waitFor(() =>
      expect(catalogPreviewMock).toHaveBeenLastCalledWith({
        model_config_id: 7,
        connection_ids: [11],
      }),
    );
    expect(catalogPreviewMock).toHaveBeenCalledTimes(2);
  });

  it("keeps the dialog and target controls locked while commit is pending", async () => {
    resetHappyPath();
    const pending = deferred<CatalogPricingCommitResponse>();
    catalogCommitMock.mockReturnValue(pending.promise);
    const { onClose, onCommitted } = await renderDialog();

    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(submit).not.toBeDisabled());
    await userEvent.click(submit);
    await waitFor(() => expect(catalogCommitMock).toHaveBeenCalledTimes(1));

    expect(submit).toBeDisabled();
    expect(submit.querySelector(".animate-spin")).not.toBeNull();
    expect(screen.getByTestId("catalog-pricing-target-11")).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByTestId("catalog-pricing-dialog")).toBeVisible();
    expect(onClose).not.toHaveBeenCalled();

    pending.resolve({
      created: true,
      updated: false,
      assigned_connection_ids: [],
      template_id: 41,
      template_name: "openai/gpt-five-part",
      revision_id: 77,
      version: 1,
      drift_confirmed: false,
    });
    await waitFor(() =>
      expect(onCommitted).toHaveBeenCalledWith("openai/gpt-five-part", 0),
    );
  });

  it("keeps the current target checked and locked for the model-detail surface", async () => {
    resetHappyPath();
    await renderDialog({
      initialConnectionIds: [11],
      lockedConnectionIds: [11],
    });
    await screen.findByTestId("catalog-pricing-cards");

    const locked = screen.getByTestId(
      "catalog-pricing-target-11",
    ) as HTMLInputElement;
    expect(locked).toBeChecked();
    expect(locked).toBeDisabled();
    expect(catalogPreviewMock.mock.calls[0][0]).toMatchObject({
      connection_ids: [11],
    });
  });

  it("keeps commit blocked after a preview failure and retries explicitly", async () => {
    resetHappyPath();
    catalogPreviewMock
      .mockRejectedValueOnce(new Error("catalog_preview_down"))
      .mockResolvedValue(previewResponse());
    await renderDialog();

    const submit = await screen.findByTestId("catalog-pricing-submit");
    expect(await screen.findByText("catalog_preview_down")).toBeInTheDocument();
    expect(submit).toBeDisabled();
    expect(screen.getByTestId("catalog-pricing-blockers")).toHaveTextContent(
      "尚未取得有效预览，无法提交",
    );
    expect(catalogCommitMock).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(catalogPreviewMock).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(submit).not.toBeDisabled());
  });

  it("disables commit with an explicit reason when the plan is not lossless", async () => {
    resetHappyPath();
    catalogPreviewMock.mockResolvedValue(
      previewResponse({
        committable: false,
        preview_hash: undefined as never,
        plan: {
          template_kind: "standard",
          cards: {},
          incompatibilities: [
            { field: "cost.input_audio", reason: "audio_cost_present" },
          ],
        },
      }),
    );
    await renderDialog();

    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(submit).toBeDisabled());
    expect(
      await screen.findByTestId("catalog-pricing-blockers"),
    ).toHaveTextContent("价格不可无损表达，提交已禁用且零写入");
    expect(catalogCommitMock).not.toHaveBeenCalled();
  });

  it("requires an explicit drift acknowledgement before overwriting", async () => {
    resetHappyPath();
    catalogPreviewMock.mockResolvedValue(
      previewResponse({
        action: "drift",
        drift: true,
        template: {
          id: 41,
          name: "openai/gpt-five-part",
          version: 3,
          revision_id: 88,
          template_kind: "standard",
          updated_at: "2026-08-25T12:00:00Z",
        },
      }),
    );
    const { onCommitted } = await renderDialog();

    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(submit).toBeDisabled());
    expect(screen.getByTestId("catalog-pricing-blockers")).toHaveTextContent(
      "需先勾选人工改动确认才能提交",
    );

    await userEvent.click(screen.getByTestId("catalog-pricing-confirm-drift"));
    await waitFor(() => expect(submit).not.toBeDisabled());
    await userEvent.click(submit);

    await waitFor(() => expect(catalogCommitMock).toHaveBeenCalled());
    expect(catalogCommitMock.mock.calls[0][0]).toMatchObject({
      confirm_drift: true,
    });
    expect(onCommitted).toHaveBeenCalledWith("openai/gpt-five-part", 0);
  });

  it("requires a new drift acknowledgement after the offering changes", async () => {
    resetHappyPath();
    catalogPreviewMock.mockImplementation((request) => {
      const isSecond = request.provider_id === "codex";
      return Promise.resolve(
        previewResponse({
          offering: {
            provider_id: isSecond ? "codex" : "openai",
            catalog_model_id: isSecond ? "model-b" : "gpt-five-part",
            name: isSecond ? "Model B" : "GPT Five Part",
          },
          action: "drift",
          drift: true,
          preview_hash: isSecond ? "hash-b" : "hash-a",
          template: {
            id: isSecond ? 42 : 41,
            name: isSecond ? "codex/model-b" : "openai/gpt-five-part",
            version: 3,
            revision_id: isSecond ? 89 : 88,
            template_kind: "standard",
            updated_at: "2026-08-25T12:00:00Z",
          },
        }),
      );
    });
    const { rerenderSource } = await renderDialog();
    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(submit).toBeDisabled());

    await userEvent.click(screen.getByTestId("catalog-pricing-confirm-drift"));
    await waitFor(() => expect(submit).not.toBeDisabled());

    rerenderSource({
      kind: "coordinates",
      providerId: "codex",
      catalogModelId: "model-b",
      modelConfigId: 8,
    });
    expect(await screen.findByText("codex/model-b")).toBeInTheDocument();
    await waitFor(() => expect(submit).toBeDisabled());
    expect(
      screen.getByTestId("catalog-pricing-confirm-drift"),
    ).not.toBeChecked();
    expect(screen.getByTestId("catalog-pricing-blockers")).toHaveTextContent(
      "需先勾选人工改动确认才能提交",
    );
    expect(catalogCommitMock).not.toHaveBeenCalled();
  });

  it("drops a rejected commit preview and re-reads instead of retrying blind", async () => {
    resetHappyPath();
    catalogCommitMock.mockRejectedValueOnce(
      new Error("models_dev_pricing_preview_stale"),
    );

    const { onCommitted } = await renderDialog();
    const submit = await screen.findByTestId("catalog-pricing-submit");
    await waitFor(() => expect(submit).not.toBeDisabled());

    await userEvent.click(submit);

    expect(await screen.findByText("读取目录价格失败")).toBeInTheDocument();
    expect(
      screen.getByText("models_dev_pricing_preview_stale"),
    ).toBeInTheDocument();
    expect(onCommitted).not.toHaveBeenCalled();
    // The stale preview was discarded and a fresh one fetched.
    await waitFor(() => expect(catalogPreviewMock).toHaveBeenCalledTimes(2));
  });
});
