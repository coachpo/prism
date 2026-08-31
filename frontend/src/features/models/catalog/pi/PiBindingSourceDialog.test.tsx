// Pi binding source dialog contract on the shared controller: nothing is
// ever preselected, stale directory evidence is readable but never
// confirmable, append failures retry the same offset without losing rows, and
// a revision rollover withdraws the list and clears the selection.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { useLocale } from "@/i18n/useLocale";
import type { PiCatalogSearchResponse } from "@/lib/types";
import { PiBindingSourceDialog } from "./PiBindingSourceDialog";
import {
  piViewFromModelRead,
  usePiBindingController,
} from "./usePiBindingController";

vi.mock("@/lib/api/modelExport", () => ({
  searchModelPiCatalog: vi.fn(),
  bindModelPi: vi.fn(),
  refreshModelPiPreview: vi.fn(),
  refreshModelPiCommit: vi.fn(),
  putModelPiOverride: vi.fn(),
  clearModelPiOverride: vi.fn(),
  unbindModelPi: vi.fn(),
  fetchModelPi: vi.fn(),
  fetchModelExportSource: vi.fn(),
  renderModelExport: vi.fn(),
}));

import { searchModelPiCatalog } from "@/lib/api/modelExport";

function searchPage(
  revision: string,
  status: "fresh" | "stale",
  offset: number,
  count: number,
  total: number,
): PiCatalogSearchResponse {
  const results = Array.from({ length: count }, (_, index) => ({
    provider_id: "openai",
    model_id: `gpt-${offset + index}`,
    api: "openai-responses",
    name: `GPT ${offset + index}`,
  }));
  return {
    query: "gpt",
    api: "openai-responses",
    limit: 20,
    offset,
    total,
    returned: count,
    truncated: offset + count < total,
    selected: false,
    catalog: { status, revision, etag: "etag-1" },
    fetched_at: "2026-08-31T00:00:00Z",
    checked_at: "2026-08-31T00:00:00Z",
    export_identity: {
      model_config_id: 7,
      model_id: "codex/gpt-x",
      api: "openai-responses",
      provider_id_source: "operator_input",
    },
    results,
  };
}

function DialogHarness({
  reconcile,
  actionsBlocked,
}: {
  reconcile: () => Promise<void>;
  actionsBlocked?: boolean;
}) {
  const controller = usePiBindingController({ reconcile, actionsBlocked });
  const copy = useLocale().messages.modelExportPage as Record<string, string>;
  const view = piViewFromModelRead({
    model: {
      model_config_id: 7,
      model_id: "codex/gpt-x",
      pi_api: "openai-responses",
    },
    catalog: { status: "fresh", revision: "rev-src" },
    candidates: [],
    binding: { bound: false, source: null, override: null, effective: null },
    binding_status: "unbound",
    binding_renderable: false,
  });
  return (
    <PiBindingSourceDialog
      copy={copy}
      view={view}
      onClose={() => {}}
      controller={controller}
    />
  );
}

function ModelSwitchHarness() {
  const [modelConfigId, setModelConfigId] = useState(7);
  const controller = usePiBindingController({ reconcile: vi.fn() });
  const copy = useLocale().messages.modelExportPage as Record<string, string>;
  const modelId = modelConfigId === 7 ? "codex/gpt-x" : "codex/gpt-y";
  const view = piViewFromModelRead({
    model: {
      model_config_id: modelConfigId,
      model_id: modelId,
      pi_api: "openai-responses",
    },
    catalog: { status: "fresh", revision: `rev-${modelConfigId}` },
    candidates: [],
    binding: { bound: false, source: null, override: null, effective: null },
    binding_status: "unbound",
    binding_renderable: false,
  });
  return (
    <>
      <button type="button" onClick={() => setModelConfigId(8)}>
        switch model
      </button>
      <PiBindingSourceDialog
        copy={copy}
        view={view}
        onClose={() => {}}
        controller={controller}
      />
    </>
  );
}

// The dialog copy comes from the zh-CN messages; the test harness renders the
// real provider so assertions use the operator-visible strings.
function renderDialog(reconcile = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <DialogHarness reconcile={reconcile} />
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

function confirmButton() {
  return screen.getByRole("button", { name: "应用绑定" });
}

async function typeAndSearch(
  user: ReturnType<typeof userEvent.setup>,
  query: string,
) {
  await user.type(
    screen.getByRole("textbox", { name: "目录 model_id 片段" }),
    query,
  );
  await user.click(screen.getByRole("button", { name: "搜索目录" }));
}

describe("PiBindingSourceDialog", () => {
  it("never preselects a search hit: confirm stays inert until an explicit choice", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog).mockResolvedValueOnce(
      searchPage("rev-1", "fresh", 0, 2, 2),
    );
    renderDialog();

    expect(confirmButton()).toBeDisabled();
    await typeAndSearch(user, "gpt");
    await screen.findByRole("option", { name: /openai\/gpt-0/ });

    // Two hits are visible and NEITHER is selected; confirm stays inert.
    expect(
      screen.getByRole("option", { name: /openai\/gpt-1/ }),
    ).toBeInTheDocument();
    expect(confirmButton()).toBeDisabled();
    expect(bindSpy()).not.toHaveBeenCalled();
  });

  it("a single search hit is still only evidence: one hit does not select", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog).mockResolvedValueOnce(
      searchPage("rev-1", "fresh", 0, 1, 1),
    );
    renderDialog();
    await typeAndSearch(user, "gpt");
    await screen.findByRole("option", { name: /openai\/gpt-0/ });
    expect(confirmButton()).toBeDisabled();
  });

  it("keeps stale directory evidence readable but never confirmable", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog).mockResolvedValueOnce(
      searchPage("rev-stale", "stale", 0, 1, 1),
    );
    renderDialog();
    await typeAndSearch(user, "gpt");
    await screen.findByRole("option", { name: /openai\/gpt-0/ });

    // The stale evidence is visible...
    expect(
      screen.getByText(/last-known-good|目录证据仅供查看|stale/),
    ).toBeInTheDocument();
    // ...but the confirm stays inert and nothing was written.
    expect(confirmButton()).toBeDisabled();
    expect(bindSpy()).not.toHaveBeenCalled();
  });

  it("append failure keeps rows and retries the same offset exactly once", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog)
      .mockResolvedValueOnce(searchPage("rev-1", "fresh", 0, 20, 25))
      .mockRejectedValueOnce(new Error("append failed"))
      .mockResolvedValueOnce(searchPage("rev-1", "fresh", 20, 5, 25));
    renderDialog();
    await typeAndSearch(user, "gpt");
    await screen.findByRole("option", { name: /openai\/gpt-19/ });
    expect(screen.queryByRole("option", { name: /openai\/gpt-20/ })).toBeNull();

    await user.click(screen.getByTestId("pi-directory-load-more"));
    await screen.findByText("append failed");
    // Rows stay on screen; the failure is the local retry control.
    expect(
      screen.getByRole("option", { name: /openai\/gpt-19/ }),
    ).toBeInTheDocument();

    // A synchronous double click must still issue exactly one retry read:
    // the pager's single-flight guard drops the second dispatch.
    const retryControl = screen.getByTestId("pi-directory-load-more");
    act(() => {
      fireEvent.click(retryControl);
      fireEvent.click(retryControl);
    });
    await waitFor(() => expect(searchModelPiCatalog).toHaveBeenCalledTimes(3));
    expect(searchModelPiCatalog).toHaveBeenLastCalledWith(7, {
      model_id_query: "gpt",
      limit: 20,
      offset: 20,
    }, expect.any(AbortSignal));
    await screen.findByRole("option", { name: /openai\/gpt-24/ });
    expect(
      screen.queryByTestId("pi-directory-load-more"),
    ).not.toBeInTheDocument();
  });

  it("a revision rollover withdraws the list, clears the selection, and re-reads offset 0", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog)
      .mockResolvedValueOnce(searchPage("rev-1", "fresh", 0, 20, 25))
      // The append answers from a different revision.
      .mockResolvedValueOnce(searchPage("rev-2", "fresh", 20, 5, 30))
      .mockResolvedValueOnce(searchPage("rev-2", "fresh", 0, 20, 30));
    renderDialog();
    await typeAndSearch(user, "gpt");
    const option = await screen.findByRole("option", { name: /openai\/gpt-0/ });
    await user.click(option);
    expect(confirmButton()).toBeEnabled();

    // The append lands with a different revision: the mixed group is
    // withdrawn and the pager restarts from offset 0 of the new revision.
    await user.click(screen.getByTestId("pi-directory-load-more"));
    expect(
      await screen.findByTestId("pi-directory-rollover"),
    ).toBeInTheDocument();
    await screen.findByRole("option", { name: /openai\/gpt-0/ });
    await waitFor(() =>
      expect(searchModelPiCatalog).toHaveBeenLastCalledWith(7, {
        model_id_query: "gpt",
        limit: 20,
        offset: 0,
      }, expect.any(AbortSignal)),
    );
    // The selection died with the withdrawn revision: confirm is inert again.
    expect(confirmButton()).toBeDisabled();
    expect(bindSpy()).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "知道了" }));
    expect(screen.queryByTestId("pi-directory-rollover")).toBeNull();
  });

  it("binds an explicitly chosen coordinate with the layer's revision evidence", async () => {
    const user = userEvent.setup();
    const reconcile = vi.fn();
    vi.mocked(searchModelPiCatalog).mockResolvedValueOnce(
      searchPage("rev-9", "fresh", 0, 1, 1),
    );
    (bindModelPiMock() as ReturnType<typeof vi.fn>).mockResolvedValue({
      bound: true,
      source: null,
      override: null,
      effective: null,
    });
    renderDialog(reconcile);
    await typeAndSearch(user, "gpt");
    const option = await screen.findByRole("option", { name: /openai\/gpt-0/ });
    await user.click(option);
    await user.click(confirmButton());

    await waitFor(() =>
      expect(bindModelPiMock()).toHaveBeenCalledWith(7, {
        provider_id: "openai",
        catalog_model_id: "gpt-0",
        expected_catalog_revision: "rev-9",
        expected_prism_model_id: "codex/gpt-x",
        expected_pi_api: "openai-responses",
      }),
    );
  });

  it("withdraws the previous model's search rows when a shared controller host changes model", async () => {
    const user = userEvent.setup();
    vi.mocked(searchModelPiCatalog).mockResolvedValueOnce(
      searchPage("rev-1", "fresh", 0, 1, 1),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <LocaleProvider>
          <ModelSwitchHarness />
        </LocaleProvider>
      </QueryClientProvider>,
    );
    await typeAndSearch(user, "gpt");
    expect(
      await screen.findByRole("option", { name: /openai\/gpt-0/ }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText("switch model"));
    expect(screen.queryByRole("option", { name: /openai\/gpt-0/ })).toBeNull();
    expect(screen.getAllByText("codex/gpt-y").length).toBeGreaterThan(0);
    expect(confirmButton()).toBeDisabled();
  });
});

import { bindModelPi } from "@/lib/api/modelExport";

// Mock calls accumulate across tests in a file; every test asserts its own
// call sequence, so clear the recorded calls before each one.
beforeEach(() => {
  vi.mocked(searchModelPiCatalog).mockClear();
  vi.mocked(bindModelPi).mockClear();
});

function bindSpy() {
  return vi.mocked(bindModelPi);
}
function bindModelPiMock() {
  return vi.mocked(bindModelPi);
}
