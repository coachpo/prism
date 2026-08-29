import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { ExportRenderResponse, ExportSourceResponse } from "./exportTypes";
import { ModelExportPage } from "./ModelExportPage";
import { fetchModelExportSource } from "@/lib/api/modelExport";
import { ExportResultSheet } from "./ExportResultSheet";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => ({}),
}));

const sourceFixture = (
  overrides?: Partial<ExportSourceResponse>,
): ExportSourceResponse => ({
  target_version: "0.84.3",
  catalog: {
    status: "fresh",
    revision: "rev-1",
    minimum_version: "0.80.7",
    etag: "etag-1",
  },
  source_digest: "a".repeat(64),
  models: [
    {
      model_config_id: 3,
      model_id: "gpt-x",
      api_family: "openai",
      display_name: "GPT Friendly",
      is_enabled: true,
      selectable: true,
      openai_accepted_format: "dual_native",
      prism_metadata: {},
      merged_metadata: { name: "gpt-x", reasoning: true },
      metadata_provenance: {},
      missing_metadata: [],
      platform_completeness: {
        metadata_fields: { name: true, reasoning: true },
        cost_exportable: true,
      },
      targets: [
        {
          terminal_target_id: 11,
          position: 0,
          endpoint_id: 21,
          endpoint_name: "primary",
        },
      ],
      price_risk: { exportable: true },
      pi_candidates: [
        {
          provider_id: "openai",
          model_id: "gpt-x",
          api: "openai-responses",
          name: "GPT X",
        },
      ],
      pi_selected: {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
      },
      candidate_status: "single",
      pi_binding_status: "bound",
      pi_bind_source: "single_candidate",
    },
    {
      model_config_id: 5,
      model_id: "glm-5.2",
      api_family: "openai",
      display_name: null,
      is_enabled: true,
      selectable: true,
      openai_accepted_format: "chat_completions_only",
      prism_metadata: {},
      merged_metadata: {},
      metadata_provenance: {},
      missing_metadata: ["name"],
      platform_completeness: {
        metadata_fields: { name: false, reasoning: false },
        cost_exportable: false,
      },
      targets: [],
      price_risk: { exportable: false, warning_codes: ["price_no_template"] },
      warnings: ["metadata_incomplete"],
      pi_candidates: [],
      candidate_status: "not_in_catalog",
      pi_binding_status: "unbound",
    },
  ],
  ...overrides,
});

vi.mock("@/lib/api/modelExport", () => ({
  fetchModelExportSource: vi.fn(() => Promise.resolve(sourceFixture())),
  renderModelExport: vi.fn(() =>
    Promise.resolve({
      target_version: "0.84.3",
      catalog: { status: "fresh", revision: "rev-1" },
      content: "{}\n",
      content_sha256: "c".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
    }),
  ),
  bindModelPi: vi.fn(),
  refreshModelPiPreview: vi.fn(),
  refreshModelPiCommit: vi.fn(),
  putModelPiOverride: vi.fn(),
  clearModelPiOverride: vi.fn(),
  unbindModelPi: vi.fn(),
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <TooltipProvider>
          <ModelExportPage />
        </TooltipProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

describe("ModelExportPage Pi-only", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchModelExportSource).mockImplementation(() =>
      Promise.resolve(sourceFixture()),
    );
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
  });

  it("adopts selectable models on first load", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByRole("checkbox", { name: "gpt-x" })).toBeChecked();
  });

  it("shows catalog status and a bound candidate's coordinate", async () => {
    renderPage();
    await screen.findByText(/目录状态/);
    expect(screen.getAllByText("a".repeat(64)).length).toBeGreaterThan(0);
    expect(screen.getByText("openai/gpt-x (openai-responses)")).toBeVisible();
  });

  it("offers a bind action for an unbound single candidate row", async () => {
    renderPage();
    const row = await screen.findByTestId("export-row-5");
    expect(row.textContent).toContain("未收录");
  });
});

describe("ExportResultSheet Pi-only", () => {
  it("copies full content and Pi merge fragment", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      catalog: { status: "fresh", revision: "rev-1" },
      content: '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
    };
    const { unmount } = render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>,
    );
    await user.click(screen.getByRole("button", { name: "复制" }));
    expect(writeText).toHaveBeenLastCalledWith(result.content);
    unmount();
  });
});
