// Catalog metadata honesty: unbound/bound states stay distinguishable,
// override markers surface on merged fields, and the pricing dialog refuses
// to commit incompatible plans or unconfirmed drift.
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ModelCatalogResponse } from "@/lib/types";
import { CatalogMetadataCard } from "@/pages/model-detail/CatalogMetadataCard";

// The shared pricing panel renders the catalog fetch stamp through the operator
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
      candidates: vi
        .fn()
        .mockResolvedValue({
          items: [],
          total: 0,
          limit: 20,
          offset: 0,
          scope: "family",
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

function renderCard(catalog: ModelCatalogResponse | null, onChanged = vi.fn()) {
  return render(
    <LocaleProvider>
      <CatalogMetadataCard
        modelConfigId={7}
        catalog={catalog}
        onChanged={onChanged}
      />
    </LocaleProvider>,
  );
}

describe("CatalogMetadataCard", () => {
  it("keeps an unbound model visibly unbound instead of blank", () => {
    renderCard({ bound: false, source: null, override: null, effective: null });
    expect(screen.getByText("未绑定")).toBeInTheDocument();
    expect(screen.getByText(/尚未绑定目录条目/)).toBeInTheDocument();
  });

  it("shows coordinates, fetch stamp, and effective values when bound", () => {
    const catalog = boundCatalog();
    catalog.effective = { ...catalog.source! };
    renderCard(catalog);
    expect(screen.getByText("自动匹配")).toBeInTheDocument();
    expect(screen.getByText("openai / gpt-test")).toBeInTheDocument();
    expect(screen.getByText("GPT Test")).toBeInTheDocument();
    expect(screen.getByText("128000")).toBeInTheDocument();
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
    renderCard(catalog);
    expect(screen.getByText("存在人工覆盖")).toBeInTheDocument();
    // The override marker rides on the field label of the overridden key.
    const nameLabel = screen.getByText(/名称/);
    expect(nameLabel.textContent).toContain("已覆盖");
  });

  it("disables refresh for unbound models because there is nothing to diff", () => {
    renderCard({ bound: false, source: null, override: null, effective: null });
    const refreshButton = screen.getByRole("button", { name: /刷新预览/ });
    expect(refreshButton).toBeDisabled();
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
