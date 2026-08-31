// Catalog metadata honesty: unbound/bound/stale/error states stay
// distinguishable, override markers surface on merged fields, and the pricing
// dialog refuses to commit incompatible plans or unconfirmed drift.
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogResponse } from "@/lib/types";
import { ModelsDevCatalogPanel } from "@/features/models/detail/ModelsDevCatalogPanel";
import { CatalogOverrideDialog } from "@/pages/model-detail/CatalogOverrideDialog";
import {
  useModelCatalog,
  type ModelCatalogView,
} from "@/pages/model-detail/useModelCatalog";

// The shared pricing panel renders the catalog fetch stamp through the operator
// timezone, which otherwise reads live settings over the network.
vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    timezone: "UTC",
    format: (iso: string) => `UTC:${iso}`,
    loading: false,
    refresh: async () => "UTC",
  }),
}));

vi.mock("@/lib/api/models", () => ({
  models: {
    catalog: {
      get: vi.fn(),
      matchPreview: vi.fn(),
      bind: vi.fn(),
      refreshPreview: vi.fn(),
      refreshCommit: vi.fn(),
      putOverride: vi.fn(),
      clearOverride: vi.fn(),
      unbind: vi.fn(),
      candidates: vi.fn().mockResolvedValue({
        items: [],
        total: 0,
        limit: 20,
        offset: 0,
        scope: "family",
        catalog_revision: '"rev-1"',
      }),
    },
  },
}));

function boundCatalog(
  overrides?: Partial<ModelCatalogResponse>,
): ModelCatalogResponse {
  return {
    bound: true,
    match_source: "unique_match",
    provider_id: "openai",
    catalog_model_id: "gpt-test",
    catalog_revision: '"rev-1"',
    fetched_at: "2026-08-25T12:00:00Z",
    updated_at: "2026-08-25T12:00:00Z",
    source: {
      name: "GPT Test",
      description: "fixture",
      family: "gpt",
      release_date: "2026-01-15",
      last_updated: "2026-02-20",
      knowledge: null,
      attachment: false,
      reasoning: true,
      tool_call: true,
      structured_output: null,
      temperature: null,
      modalities_input: ["text"],
      modalities_output: ["text"],
      limit_context: 128000,
      limit_input: null,
      limit_output: 16384,
      open_weights: false,
      status: null,
    },
    override: null,
    effective: null,
    ...overrides,
  } as ModelCatalogResponse;
}

function catalogView(
  overrides: Partial<ModelCatalogView> & {
    catalog: ModelCatalogResponse | null;
  },
): ModelCatalogView {
  return {
    loading: false,
    refreshing: false,
    failed: false,
    error: null,
    hasLastGood: overrides.catalog !== null,
    lastSuccessfulAt:
      overrides.catalog !== null ? "2026-08-25T12:00:00Z" : null,
    refresh: vi.fn(),
    ...overrides,
  };
}

function renderCard(
  view: ModelCatalogView,
  onChanged = vi.fn(),
  props?: { prismModelId?: string; apiFamily?: string },
) {
  return render(
    <LocaleProvider>
      <ModelsDevCatalogPanel
        modelConfigId={7}
        prismModelId={props?.prismModelId ?? "gpt-test"}
        apiFamily={props?.apiFamily ?? "openai"}
        catalogView={view}
        onChanged={onChanged}
      />
    </LocaleProvider>,
  );
}

describe("useModelCatalog", () => {
  it("retains same-model last-good data after a failed re-read and clears stale on recovery", async () => {
    const catalog = boundCatalog({ effective: boundCatalog().source });
    let rejectRefresh!: (cause: unknown) => void;
    const failedRefresh = new Promise<ModelCatalogResponse>((_resolve, reject) => {
      rejectRefresh = reject;
    });
    vi.mocked(modelsApi.catalog.get)
      .mockResolvedValueOnce(catalog)
      .mockReturnValueOnce(failedRefresh)
      .mockResolvedValueOnce({ ...catalog, fetched_at: "2026-08-26T12:00:00Z" });
    const { result } = renderHook(() => useModelCatalog(7, 0));

    await waitFor(() => expect(result.current.catalog).toBe(catalog));
    act(() => result.current.refresh());
    expect(result.current.refreshing).toBe(true);
    expect(result.current.catalog).toBe(catalog);
    await act(async () => rejectRefresh(new Error("refresh failed")));
    await waitFor(() => expect(result.current.failed).toBe(true));
    expect(result.current.catalog).toBe(catalog);
    expect(result.current.hasLastGood).toBe(true);
    expect(result.current.error).toBe("refresh failed");

    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.failed).toBe(false));
    expect(result.current.catalog?.fetched_at).toBe("2026-08-26T12:00:00Z");
    expect(result.current.hasLastGood).toBe(true);
  });

  it("withdraws the previous model immediately when the model id changes", async () => {
    const nextRead = new Promise<ModelCatalogResponse>(() => {});
    vi.mocked(modelsApi.catalog.get)
      .mockResolvedValueOnce(boundCatalog())
      .mockReturnValueOnce(nextRead);
    const { result, rerender } = renderHook(
      ({ modelConfigId }) => useModelCatalog(modelConfigId, 0),
      { initialProps: { modelConfigId: 7 } },
    );
    await waitFor(() => expect(result.current.catalog?.bound).toBe(true));
    rerender({ modelConfigId: 8 });
    expect(result.current.catalog).toBeNull();
    expect(result.current.loading).toBe(true);
    expect(result.current.hasLastGood).toBe(false);
  });

  it("keeps a first-read retry in loading without claiming last-good refresh", async () => {
    const retryRead = new Promise<ModelCatalogResponse>(() => {});
    vi.mocked(modelsApi.catalog.get)
      .mockRejectedValueOnce(new Error("first read failed"))
      .mockReturnValueOnce(retryRead);
    const { result } = renderHook(() => useModelCatalog(7, 0));
    await waitFor(() => expect(result.current.failed).toBe(true));

    act(() => result.current.refresh());
    expect(result.current.loading).toBe(true);
    expect(result.current.refreshing).toBe(false);
    expect(result.current.catalog).toBeNull();
    expect(result.current.hasLastGood).toBe(false);
  });
});

describe("ModelsDevCatalogPanel", () => {
  it("keeps an unbound model visibly unbound instead of blank", () => {
    renderCard(
      catalogView({
        catalog: {
          bound: false,
          source: null,
          override: null,
          effective: null,
        } as ModelCatalogResponse,
      }),
    );
    expect(screen.getByText("未绑定")).toBeInTheDocument();
    expect(screen.getByText(/尚未绑定 models\.dev 目录条目/)).toBeInTheDocument();
  });

  it("shows coordinates, timezone fetch stamp, and effective values when bound", () => {
    const catalog = boundCatalog();
    catalog.effective = { ...catalog.source! };
    renderCard(catalogView({ catalog }));
    expect(screen.getByText("自动匹配")).toBeInTheDocument();
    expect(screen.getByText("openai / gpt-test")).toBeInTheDocument();
    expect(screen.getByText("GPT Test")).toBeInTheDocument();
    expect(screen.getByText("128000")).toBeInTheDocument();
    // The fetch stamp goes through the global timezone formatter, never a raw
    // toLocaleString.
    expect(screen.getByText(/UTC:2026-08-25T12:00:00Z/)).toBeInTheDocument();
  });

  it("unbinds only with the currently displayed coordinate and updated_at snapshot", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    vi.mocked(modelsApi.catalog.unbind).mockResolvedValue({
      bound: false,
      source: null,
      override: null,
      effective: null,
    });
    renderCard(catalogView({ catalog: boundCatalog() }), onChanged);

    await user.click(
      screen.getByRole("button", { name: "models.dev 绑定操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "解绑目录" }));
    await user.click(screen.getByTestId("models-dev-unbind-confirm"));

    await waitFor(() =>
      expect(modelsApi.catalog.unbind).toHaveBeenCalledWith(7, {
        expected_provider_id: "openai",
        expected_catalog_model_id: "gpt-test",
        expected_binding_updated_at: "2026-08-25T12:00:00Z",
      }),
    );
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("clears overrides only with the displayed coordinate and updated_at snapshot", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    vi.mocked(modelsApi.catalog.clearOverride).mockResolvedValue(
      boundCatalog({ override: null }),
    );
    renderCard(catalogView({ catalog: boundCatalog() }), onChanged);

    await user.click(
      screen.getByRole("button", { name: "models.dev 绑定操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "编辑覆盖" }));
    await user.click(
      screen.getByRole("button", { name: "清除全部覆盖" }),
    );

    await waitFor(() =>
      expect(modelsApi.catalog.clearOverride).toHaveBeenCalledWith(7, {
        expected_provider_id: "openai",
        expected_catalog_model_id: "gpt-test",
        expected_binding_updated_at: "2026-08-25T12:00:00Z",
      }),
    );
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("keeps a stale-snapshot unbind conflict inline and authoritatively re-reads", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    vi.mocked(modelsApi.catalog.unbind).mockRejectedValueOnce(
      new Error("models_dev_binding_stale"),
    );
    renderCard(catalogView({ catalog: boundCatalog() }), onChanged);
    await user.click(
      screen.getByRole("button", { name: "models.dev 绑定操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "解绑目录" }));
    await user.click(screen.getByTestId("models-dev-unbind-confirm"));

    const dialog = screen.getByRole("dialog", {
      name: "解绑 models.dev 目录绑定",
    });
    expect(
      await within(dialog).findByText("models_dev_binding_stale"),
    ).toBeVisible();
    expect(dialog).toBeVisible();
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("renders a loading surface for the first read instead of unbound", () => {
    renderCard(
      catalogView({ catalog: null, loading: true, hasLastGood: false }),
    );
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(
      screen.getByText("正在读取 models.dev 目录绑定…"),
    ).toBeInTheDocument();
    expect(screen.queryByText("未绑定")).not.toBeInTheDocument();
  });

  it("renders error+retry for a failed first read and never claims unbound", async () => {
    const user = userEvent.setup();
    const refresh = vi.fn();
    renderCard(
      catalogView({
        catalog: null,
        failed: true,
        error: "models_dev_catalog_unavailable: boom",
        hasLastGood: false,
        refresh,
      }),
    );
    expect(screen.getByTestId("catalog-read-error")).toBeInTheDocument();
    expect(screen.queryByText("未绑定")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /重试读取/ }));
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("keeps last-good metadata visible with one staleness badge and read retry", async () => {
    const user = userEvent.setup();
    const refresh = vi.fn();
    const catalog = boundCatalog();
    catalog.effective = { ...catalog.source! };
    renderCard(
      catalogView({
        catalog,
        failed: true,
        error: "refresh failed",
        lastSuccessfulAt: "2026-08-25T12:00:00Z",
        refresh,
      }),
    );
    // The last good metadata stays on screen...
    expect(screen.getByText("GPT Test")).toBeInTheDocument();
    expect(screen.getByText("openai / gpt-test")).toBeInTheDocument();
    // ...with exactly one staleness badge that carries the last success stamp.
    expect(screen.getAllByTestId("catalog-read-stale")).toHaveLength(1);
    expect(screen.getByText(/UTC:2026-08-25T12:00:00Z/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新绑定" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "重试读取" }));
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("marks overridden fields so source and manual values stay distinguishable", () => {
    const catalog = boundCatalog({
      override: {
        name: "Operator Name",
        description: null,
        family: null,
        release_date: null,
        last_updated: null,
        knowledge: null,
        attachment: null,
        reasoning: null,
        tool_call: null,
        structured_output: null,
        temperature: null,
        modalities_input: null,
        modalities_output: null,
        limit_context: null,
        limit_input: null,
        limit_output: null,
        open_weights: null,
        status: null,
      },
    });
    catalog.effective = { ...catalog.source!, name: "Operator Name" };
    renderCard(catalogView({ catalog }));
    expect(screen.getByText("存在人工覆盖")).toBeInTheDocument();
    // The override marker rides on the field label of the overridden key.
    const nameLabel = screen.getByText(/名称/);
    expect(nameLabel.textContent).toContain("已覆盖");
  });

  it("does not offer bound-only actions for an unbound model", () => {
    renderCard(
      catalogView({
        catalog: {
          bound: false,
          source: null,
          override: null,
          effective: null,
        } as ModelCatalogResponse,
      }),
    );
    expect(
      screen.queryByRole("button", { name: /models\.dev 绑定操作/ }),
    ).not.toBeInTheDocument();
  });
});

describe("CatalogOverrideDialog binding snapshot", () => {
  it("keeps an open mutation targeted at its opening snapshot after a re-read", async () => {
    const user = userEvent.setup();
    const initial = boundCatalog();
    const rebound = boundCatalog({
      provider_id: "azure",
      catalog_model_id: "gpt-new",
      updated_at: "2026-08-25T12:01:00Z",
    });
    vi.mocked(modelsApi.catalog.clearOverride).mockResolvedValue(rebound);
    const runAction = async (
      action: () => Promise<unknown>,
      done?: () => void,
      onError?: (message: string) => void,
    ) => {
      try {
        await action();
        done?.();
      } catch (cause) {
        onError?.(cause instanceof Error ? cause.message : String(cause));
      }
    };

    const rendered = render(
      <LocaleProvider>
        <CatalogOverrideDialog
          modelConfigId={7}
          catalog={initial}
          busy={false}
          onClose={() => {}}
          runAction={runAction}
        />
      </LocaleProvider>,
    );
    rendered.rerender(
      <LocaleProvider>
        <CatalogOverrideDialog
          modelConfigId={7}
          catalog={rebound}
          busy={false}
          onClose={() => {}}
          runAction={runAction}
        />
      </LocaleProvider>,
    );
    await user.click(
      screen.getByRole("button", { name: "清除全部覆盖" }),
    );

    await waitFor(() =>
      expect(modelsApi.catalog.clearOverride).toHaveBeenCalledWith(7, {
        expected_provider_id: "openai",
        expected_catalog_model_id: "gpt-test",
        expected_binding_updated_at: "2026-08-25T12:00:00Z",
      }),
    );
  });
});

describe("catalog pricing commit gating", () => {
  // The dialog's gating logic is exercised through its exported helper
  // semantics below; full interaction flows live in the Playwright journey.
  it("requires explicit confirmation before overwriting drifted templates", async () => {
    const { CatalogPricingDialog } = await import("@/features/pricing/catalog");
    const { models: managementModels } = await import("@/lib/api/models");
    type PricingApi =
      typeof import("@/lib/api/pricingTemplates").pricingTemplates;
    let previewResolver: (value: unknown) => void = () => {};
    const previewPromise = new Promise((resolve) => {
      previewResolver = resolve;
    });
    const pricingApi = (await import("@/lib/api/pricingTemplates"))
      .pricingTemplates as PricingApi;
    const previewSpy = vi
      .spyOn(pricingApi, "catalogPreview")
      .mockReturnValue(previewPromise as never);

    render(
      <LocaleProvider>
        <CatalogPricingDialog
          isOpen
          source={{ kind: "bound_model", modelConfigId: 7 }}
          title="从目录生成价格"
          targets={[]}
          initialConnectionIds={[11]}
          lockedConnectionIds={[11]}
          onClose={() => {}}
          onCommitted={() => {}}
        />
      </LocaleProvider>,
    );

    previewResolver({
      schema_version: 1,
      offering: {
        provider_id: "openai",
        catalog_model_id: "gpt-long",
        name: "GPT Long",
      },
      catalog_revision: '"rev-2"',
      fetched_at: "2026-08-25T12:00:00Z",
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
      action: "create",
      drift: false,
      committable: true,
      preview_hash: "hash-1",
      targets: [],
      reporting_currency_code: "USD",
    });

    await waitFor(() =>
      expect(screen.getByTestId("catalog-pricing-submit")).not.toBeDisabled(),
    );
    expect(managementModels).toBeTruthy();

    // Incompatible plans disable the submit button entirely.
    previewSpy.mockRestore();
  });

  it("surfaces stable incompatibility reasons instead of fake prices", async () => {
    const { CatalogPricingDialog } = await import("@/features/pricing/catalog");
    const pricingApi = (await import("@/lib/api/pricingTemplates"))
      .pricingTemplates;
    vi.spyOn(pricingApi, "catalogPreview").mockResolvedValue({
      schema_version: 1,
      offering: {
        provider_id: "openai",
        catalog_model_id: "gpt-audio",
        name: "GPT Audio",
      },
      catalog_revision: '"rev-3"',
      fetched_at: "2026-08-25T12:00:00Z",
      plan: {
        template_kind: "standard",
        cards: {},
        incompatibilities: [
          { field: "cost.input_audio", reason: "audio_cost_present" },
        ],
      },
      action: "create",
      drift: false,
      committable: false,
      preview_hash: undefined as never,
      targets: [],
      reporting_currency_code: "USD",
      catalog_currency: "USD",
      pricing_unit: "PER_1M",
    } as never);

    render(
      <LocaleProvider>
        <CatalogPricingDialog
          isOpen
          source={{ kind: "bound_model", modelConfigId: 7 }}
          title="从目录生成价格"
          targets={[]}
          initialConnectionIds={[11]}
          lockedConnectionIds={[11]}
          onClose={() => {}}
          onCommitted={() => {}}
        />
      </LocaleProvider>,
    );

    // The stable reason reaches the operator as a label, never as a raw enum
    // key, and the field path stays visible as evidence.
    await waitFor(() =>
      expect(
        screen.getByText("目录条目含音频计价，Prism 无对应价格种类"),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("cost.input_audio")).toBeInTheDocument();
    expect(screen.queryByText("audio_cost_present")).not.toBeInTheDocument();
    expect(screen.getByTestId("catalog-pricing-submit")).toBeDisabled();
  });
});
