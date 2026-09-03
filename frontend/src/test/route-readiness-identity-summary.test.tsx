// RouteReadinessCard direct identity summary: the card must aggregate only
// DIRECT facts from the mixed access-target list, compare upstream identities
// exactly (case-sensitive) against the entry model_id, and render unknown or
// missing evidence honestly instead of backfilling the entry id.
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Connection, ModelConfig } from "@/lib/types";
import { RouteReadinessCard } from "@/pages/model-detail/RouteReadinessCard";

const ENTRY_MODEL_ID = "Entry-A";

function makeConnection(id: number, upstreamModelId: string | null): Connection {
  return {
    id,
    profile_id: 1,
    model_config_id: 1,
    api_family: "openai",
    endpoint_id: 100 + id,
    endpoint: { id: 100 + id, name: `endpoint-${id}`, base_url: "https://x", has_api_key: false, api_key_fingerprint: null, api_key_updated_at: null, config_revision: 1, created_at: "t", updated_at: "t" },
    is_active: true,
    priority: 0,
    name: `conn-${id}`,
    auth_type: null,
    upstream_model_id: upstreamModelId,
    custom_headers: null,
    custom_headers_redacted: null,
    custom_request_parameters: null,
    routing_schedule: null,
    routing_schedule_state: null,
    openai_text_capability: null,
    openai_image_capability: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: "t",
    updated_at: "t",
  } as unknown as Connection;
}

function makeTarget(id: number, targetType: "model" | "connection", connection: Connection | null): ModelConfig["access_targets"][number] {
  return {
    id,
    target_type: targetType,
    target_model_id: targetType === "model" ? "child-model" : null,
    connection_id: connection?.id ?? null,
    terminal_target_id: connection?.id ?? null,
    position: id,
    is_enabled: true,
    target_model: targetType === "model" ? { id: 500 + id, model_id: "child-model" } : null,
    connection,
    terminal_target: connection,
    created_at: "t",
    updated_at: "t",
  } as unknown as ModelConfig["access_targets"][number];
}

function makeModel(targets: ModelConfig["access_targets"]): ModelConfig {
  return {
    id: 1,
    profile_id: 1,
    api_family: "openai",
    model_id: ENTRY_MODEL_ID,
    display_name: "Entry A",
    openai_accepted_format: null,
    openai_image_operations: null,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: targets,
    is_enabled: true,
    created_at: "t",
    updated_at: "t",
  } as unknown as ModelConfig;
}

function renderCard(model: ModelConfig) {
  render(
    <LocaleProvider>
      <RouteReadinessCard
        diagnosticsView={{ kind: "idle" }}
        model={model}
        onRetryDiagnostics={vi.fn()}
      />
    </LocaleProvider>,
  );
}

function factValue(label: string): string {
  const panels = screen.getAllByText(label);
  const panel = panels.at(-1)!.closest("[data-slot=inset-panel]") ?? panels.at(-1)!.parentElement!;
  return panel.textContent ?? "";
}

describe("RouteReadinessCard direct identity summary", () => {
  it("names the entry model ID at the top of the card", () => {
    renderCard(makeModel([]));
    expect(screen.getAllByText(ENTRY_MODEL_ID).length).toBeGreaterThan(0);
  });

  it("counts distinct upstream identities, excluding unknown evidence", () => {
    renderCard(makeModel([
      makeTarget(1, "connection", makeConnection(11, "provider/X")),
      makeTarget(2, "connection", makeConnection(12, "provider/X")),
      makeTarget(3, "connection", makeConnection(13, "provider/Y")),
      makeTarget(4, "connection", makeConnection(14, null)),
    ]));
    expect(factValue("不同上游标识")).toBe("不同上游标识 ?2");
  });

  it("compares decoupling against the entry ID case-sensitively", () => {
    renderCard(makeModel([
      makeTarget(1, "connection", makeConnection(11, "entry-a")),
      makeTarget(2, "connection", makeConnection(12, "provider/Y")),
    ]));
    expect(factValue("上游解耦")).toBe("上游解耦 ?2");
  });

  it("never counts unknown identities as decoupled and reports them separately", () => {
    renderCard(makeModel([
      makeTarget(1, "connection", makeConnection(11, null)),
      makeTarget(2, "connection", makeConnection(12, "  ")),
      makeTarget(3, "connection", makeConnection(13, ENTRY_MODEL_ID)),
    ]));
    expect(factValue("不同上游标识")).toBe("不同上游标识 ?1");
    expect(factValue("上游解耦")).toBe("上游解耦 ?0");
    expect(factValue("未知上游标识")).toBe("未知上游标识 ?2");
  });

  it("ignores Model Target rows: they are logical edges without upstream identity", () => {
    renderCard(makeModel([makeTarget(1, "model", null)]));
    // No DIRECT Terminal Targets exist, so the honest state is the explicit
    // no-direct-terminal conclusion — not fabricated zeros, not recursion.
    expect(screen.getByText("无直接终端目标")).toBeInTheDocument();
    expect(screen.getAllByText(
      "该模型配置没有直接终端目标；上游身份只由终端目标持有，模型目标是逻辑边，不在此推断。",
    ).length).toBeGreaterThan(0);
  });

  it("shows an explicit no-direct-terminal state with a reason", () => {
    renderCard(makeModel([makeTarget(1, "model", null)]));
    expect(screen.getByText("无直接终端目标")).toBeInTheDocument();
    expect(screen.getAllByText(
      "该模型配置没有直接终端目标；上游身份只由终端目标持有，模型目标是逻辑边，不在此推断。",
    ).length).toBeGreaterThan(0);
  });

  it("counts a mixed list where only Terminal Targets contribute identities", () => {
    renderCard(makeModel([
      makeTarget(1, "model", null),
      makeTarget(2, "connection", makeConnection(12, "provider/Y")),
      makeTarget(3, "connection", makeConnection(13, "provider/Z")),
    ]));
    expect(factValue("不同上游标识")).toBe("不同上游标识 ?2");
    expect(factValue("上游解耦")).toBe("上游解耦 ?2");
    expect(factValue("未知上游标识")).toBe("未知上游标识 ?0");
  });

  it("counts zero unknown identities when every direct terminal target carries a readable id", () => {
    renderCard(makeModel([
      makeTarget(1, "connection", makeConnection(11, ENTRY_MODEL_ID)),
      makeTarget(2, "connection", makeConnection(12, "provider/Y")),
    ]));
    expect(factValue("未知上游标识")).toBe("未知上游标识 ?0");
  });
});
