// The federated 外部目录来源 section: two independent source panels, each
// with its own loading/error/stale/empty surface. One source's failure must
// never mask the other source or the model configuration.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { PiModelReadResponse } from "@/lib/types";
import { ExternalCatalogSourcesSection } from "./ExternalCatalogSourcesSection";
import type { ModelCatalogView } from "@/pages/model-detail/useModelCatalog";
import {
  piViewFromModelRead,
  usePiBindingController,
} from "@/features/models/catalog/pi/usePiBindingController";

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
      matchPreview: vi.fn(),
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

vi.mock("@/lib/api/modelExport", () => ({
  fetchModelPi: vi.fn(),
}));

function piReadFixture(
  overrides?: Partial<PiModelReadResponse>,
): PiModelReadResponse {
  return {
    model: {
      model_config_id: 7,
      model_id: "codex/gpt-x",
      api_family: "openai",
      pi_api: "openai-responses",
    },
    catalog: {
      status: "fresh",
      revision: "sha256-abc",
      minimum_version: "0.84.3",
      etag: "etag-1",
      fetched_at: "2026-08-31T00:00:00Z",
      checked_at: "2026-08-31T00:00:00Z",
    },
    candidate_status: "single",
    candidates: [
      {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
        name: "GPT X",
      },
    ],
    binding_status: "unbound",
    binding_renderable: false,
    binding: { bound: false, source: null, override: null, effective: null },
    ...overrides,
  };
}

function catalogView(
  overrides: Partial<ModelCatalogView> & {
    catalog: ModelCatalogView["catalog"];
  },
): ModelCatalogView {
  return {
    loading: false,
    refreshing: false,
    failed: false,
    error: null,
    hasLastGood: overrides.catalog !== null,
    lastSuccessfulAt: null,
    refresh: vi.fn(),
    ...overrides,
  };
}

function SectionHarness({
  catalogView: view,
  piRead,
  piQueryError,
  piReadStale,
  piReadRefreshing,
  onPiRetry = () => {},
}: {
  catalogView: ModelCatalogView;
  piRead: PiModelReadResponse | null;
  piQueryError?: boolean;
  piReadStale?: boolean;
  piReadRefreshing?: boolean;
  onPiRetry?: () => void;
}) {
  const controller = usePiBindingController({
    reconcile: vi.fn(),
    actionsBlocked: piQueryError === true || piReadRefreshing === true,
  });
  return (
    <ExternalCatalogSourcesSection
      modelConfigId={7}
      prismModelId="codex/gpt-x"
      apiFamily="openai"
      catalogView={view}
      piController={controller}
      piRead={piRead}
      piReadFailed={piQueryError === true}
      piReadStale={piReadStale === true}
      piReadError={piQueryError ? "pi read failed" : null}
      piLastSuccessfulAt={piRead ? "2026-08-31T00:00:00Z" : null}
      piActionsBlocked={piQueryError === true || piReadRefreshing === true}
      onPiRetry={onPiRetry}
      piReadPending={false}
      piReadRefreshing={piReadRefreshing === true}
      piView={
        piRead
          ? piViewFromModelRead({
              model: piRead.model,
              catalog: piRead.catalog,
              candidates: piRead.candidates,
              binding: {
                bound: piRead.binding.bound,
                provider_id: piRead.binding.provider_id,
                catalog_model_id: piRead.binding.catalog_model_id,
                api: piRead.binding.api,
                prism_model_id_at_bind: piRead.binding.prism_model_id_at_bind,
                source: piRead.binding.source,
                override: piRead.binding.override,
                effective: piRead.binding.effective,
              },
              binding_status: piRead.binding_status,
              binding_renderable: piRead.binding_renderable,
            })
          : null
      }
      onCatalogChanged={() => {}}
    />
  );
}

function renderSection(props: Parameters<typeof SectionHarness>[0]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <SectionHarness {...props} />
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

describe("ExternalCatalogSourcesSection", () => {
  it("renders both source panels with their own identities", () => {
    renderSection({
      catalogView: catalogView({ catalog: null, loading: true }),
      piRead: piReadFixture(),
    });
    expect(screen.getByText("外部目录来源")).toBeInTheDocument();
    expect(screen.getByText("models.dev 元信息与价格来源")).toBeInTheDocument();
    expect(screen.getByText("pi.dev Pi 模板来源")).toBeInTheDocument();
    // The models.dev panel is loading while the pi.dev panel already serves
    // evidence: the two read states stay independent.
    expect(
      screen.getByText("正在读取 models.dev 目录绑定…"),
    ).toBeInTheDocument();
    expect(screen.getByText("codex/gpt-x")).toBeInTheDocument();
    expect(screen.getByText("最终 Pi API")).toBeInTheDocument();
  });

  it("a models.dev first-read failure does not mask the pi.dev panel", () => {
    renderSection({
      catalogView: catalogView({
        catalog: null,
        failed: true,
        error: "models_dev_catalog_unavailable",
        hasLastGood: false,
      }),
      piRead: piReadFixture(),
    });
    expect(screen.getByTestId("catalog-read-error")).toBeInTheDocument();
    // The models.dev panel never renders its own unbound hint on a failed
    // read; the pi.dev panel may still honestly report ITS unbound binding.
    expect(screen.queryByText("尚未绑定目录条目")).not.toBeInTheDocument();
    // The pi.dev panel still renders its identity evidence set.
    expect(screen.getByText("codex/gpt-x")).toBeInTheDocument();
    expect(screen.getByText("Prism model_id")).toBeInTheDocument();
    expect(screen.getByText("最终 Pi API")).toBeInTheDocument();
    expect(screen.getByText("openai-responses")).toBeInTheDocument();
  });

  it("a pi.dev read failure does not mask the models.dev panel", () => {
    renderSection({
      catalogView: catalogView({
        catalog: {
          bound: false,
          source: null,
          override: null,
          effective: null,
        },
      }),
      piRead: null,
      piQueryError: true,
    });
    expect(screen.getByTestId("pi-detail-read-error")).toBeInTheDocument();
    // The models.dev panel still renders its unbound conclusion: only its own
    // read decides that.
    expect(screen.getByText("未绑定")).toBeInTheDocument();
  });

  it("keeps the models.dev staleness badge and the pi.dev binding independent", () => {
    renderSection({
      catalogView: catalogView({
        catalog: {
          bound: false,
          source: null,
          override: null,
          effective: null,
        },
        failed: true,
        error: "refresh failed",
        lastSuccessfulAt: "2026-08-25T12:00:00Z",
      }),
      piRead: piReadFixture({
        binding_status: "bound",
        binding_renderable: true,
        binding: {
          bound: true,
          bind_source: "manual",
          provider_id: "openai",
          catalog_model_id: "gpt-x",
          api: "openai-responses",
          prism_model_id_at_bind: "codex/gpt-x",
          catalog_revision: "sha256-abc",
          fetched_at: "2026-08-31T00:00:00Z",
          updated_at: "2026-08-31T00:00:00Z",
          source: {
            name: "GPT X",
            reasoning: true,
            input: null,
            context_window: null,
            max_tokens: null,
            thinking_level_map: null,
            compat: null,
          },
          override: null,
          effective: {
            name: "GPT X",
            reasoning: true,
            input: null,
            context_window: null,
            max_tokens: null,
            thinking_level_map: null,
            compat: null,
          },
          dropped_fields: ["headers"],
        },
      }),
    });
    expect(screen.getByTestId("catalog-read-stale")).toBeInTheDocument();
    // The pi.dev binding stays healthy and renderable regardless; the
    // coordinate row also carries the cross-directory marker.
    expect(screen.getByText(/openai\/gpt-x/)).toBeInTheDocument();
    expect(screen.getByText(/headers/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /解绑/ })).toBeInTheDocument();
  });

  it("keeps last-good Pi truth visible with its own management-read retry", async () => {
    const user = userEvent.setup();
    const onPiRetry = vi.fn();
    renderSection({
      catalogView: catalogView({
        catalog: { bound: false, source: null, override: null, effective: null },
      }),
      piRead: piReadFixture(),
      piQueryError: true,
      piReadStale: true,
      onPiRetry,
    });
    expect(screen.getByTestId("pi-detail-read-stale")).toBeInTheDocument();
    expect(screen.getByText("Prism model_id")).toBeInTheDocument();
    expect(screen.getAllByText("未绑定").length).toBeGreaterThanOrEqual(2);
    await user.click(screen.getByRole("button", { name: "重试" }));
    expect(onPiRetry).toHaveBeenCalledTimes(1);
  });

  it("shows an honest Pi authoritative re-read state and blocks writes", () => {
    renderSection({
      catalogView: catalogView({
        catalog: { bound: false, source: null, override: null, effective: null },
      }),
      piRead: piReadFixture(),
      piReadRefreshing: true,
    });
    expect(screen.getByTestId("pi-detail-read-refreshing")).toHaveTextContent(
      "正在权威重读 pi.dev",
    );
    expect(screen.getByRole("button", { name: "绑定来源" })).toBeDisabled();
  });

  it("keeps the stale Pi retry visible but disabled while retrying", () => {
    renderSection({
      catalogView: catalogView({
        catalog: { bound: false, source: null, override: null, effective: null },
      }),
      piRead: piReadFixture(),
      piQueryError: true,
      piReadStale: true,
      piReadRefreshing: true,
    });
    expect(screen.getByTestId("pi-detail-read-stale")).toBeInTheDocument();
    expect(screen.getByTestId("pi-detail-read-refreshing")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试" })).toBeDisabled();
  });

  it("reports a missing bind-time identity as an integrity error instead of using the current model id", () => {
    renderSection({
      catalogView: catalogView({
        catalog: { bound: false, source: null, override: null, effective: null },
      }),
      piRead: piReadFixture({
        binding_status: "bound_drifted",
        binding_renderable: false,
        binding: {
          bound: true,
          provider_id: "openai",
          catalog_model_id: "gpt-x-alias",
          api: "openai-responses",
          catalog_revision: "sha256-old",
          source: null,
          override: null,
          effective: null,
        },
      }),
    });
    expect(screen.getByText("（绑定身份快照缺失）")).toBeInTheDocument();
    expect(screen.getByText(/冻结绑定缺少目录坐标/)).toBeInTheDocument();
  });

  it("does not render a no-op bind action when Prism has no final Pi API", () => {
    const read = piReadFixture();
    read.model.pi_api = undefined;
    renderSection({
      catalogView: catalogView({
        catalog: { bound: false, source: null, override: null, effective: null },
      }),
      piRead: read,
    });
    expect(
      screen.queryByRole("button", { name: "绑定来源" }),
    ).not.toBeInTheDocument();
  });
});
