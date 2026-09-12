// ModelExitMappingCell: the models-list exit-mapping cell rendering. The
// projection logic lives in modelExitMapping.test.ts; this suite pins what the
// operator actually sees per the DESIGN.md honesty contract: real endpoint +
// upstream identity for Terminal Targets, the logical id for Model Targets,
// reasoned `—` for missing evidence, and textual (never color-only)
// 名称相同/服务名称不同/未参与 states.
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ModelExitMappingCell } from "./ModelExitMappingCell";
import {
  ENTRY_MODEL_ID,
  entryModelListItem,
  modelTargetRow,
  routingSummary,
  terminalTargetRow,
} from "./modelExitMapping.test-fixtures";

function renderCell(model = entryModelListItem([])) {
  render(
    <LocaleProvider>
      <ModelExitMappingCell model={model} />
    </LocaleProvider>,
  );
}

describe("ModelExitMappingCell", () => {
  it("shows a Terminal Target's endpoint and actual upstream identity", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, {
          endpointName: "OpenAI Primary",
          upstreamModelId: "provider/Model-X",
        }),
      ]),
    );
    const upstream = screen.getByTitle("provider/Model-X");
    expect(upstream).toHaveTextContent("provider/Model-X");
    expect(screen.getByTitle("OpenAI Primary")).toHaveTextContent(
      "OpenAI Primary",
    );
    // The owning model-config id is never substituted for upstream evidence.
    expect(screen.queryByText(ENTRY_MODEL_ID)).not.toBeInTheDocument();
  });

  it("marks a case-sensitive upstream-only identity with the full reason", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, {
          endpointName: "OpenAI Primary",
          upstreamModelId: "entry-a",
        }),
      ]),
    );
    const decoupled = screen.getByText("服务名称不同");
    expect(decoupled).toBeInTheDocument();
    expect(decoupled).toHaveAttribute(
      "title",
      `客户端使用「${ENTRY_MODEL_ID}」，Prism 请求此服务时使用「entry-a」。`,
    );
  });

  it("marks an exact matching-case upstream identity as entry-same", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, { upstreamModelId: "Entry-A" }),
      ]),
    );
    const same = screen.getByText("名称相同");
    expect(same).toHaveAttribute(
      "title",
      `客户端与服务都使用模型名称「${ENTRY_MODEL_ID}」。`,
    );
  });

  it("keeps an exact same-id upstream as upstream-only for a non-entry config", () => {
    renderCell({
      ...entryModelListItem([
        terminalTargetRow(11, 0, { upstreamModelId: ENTRY_MODEL_ID }),
      ]),
      direct_request_enabled: false,
    });
    expect(screen.queryByText("名称相同")).not.toBeInTheDocument();
    expect(screen.getByText("服务名称不同")).toHaveAttribute(
      "title",
      `模型「${ENTRY_MODEL_ID}」仅供其他模型使用；请求此服务时使用名称「${ENTRY_MODEL_ID}」。`,
    );
  });

  it("shows a Model Target row as the logical target, not an exit", () => {
    renderCell(
      entryModelListItem([
        modelTargetRow(9, 0, { summaryModelId: "child-summary" }),
      ]),
    );
    const logical = screen.getByText("child-summary");
    expect(logical).toHaveAttribute(
      "title",
      "请求会交给模型 child-summary，再使用它已配置的服务。可打开详情查看。",
    );
  });

  it("renders missing endpoint and upstream evidence as reasoned em dashes, never the entry id", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, {
          endpointName: null,
          upstreamModelId: null,
        }),
      ]),
    );
    const dashes = screen.getAllByText("—");
    expect(dashes.length).toBe(2);
    for (const dash of dashes) {
      expect(dash.closest("[data-slot=missing-value]")).not.toBeNull();
    }
    // Screen readers get the reason, sighted operators the tooltip.
    expect(
      screen.getByText(
        "暂时无法确认此服务的模型名称，请打开模型详情检查连接设置。",
      ),
    ).toHaveClass("sr-only");
    expect(
      screen.getByText("此连接没有关联的服务信息，请打开模型详情检查。"),
    ).toHaveClass("sr-only");
    expect(screen.queryByText(ENTRY_MODEL_ID)).not.toBeInTheDocument();
  });

  it("keeps a disabled row visible and flags 未参与", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, {
          isEnabled: false,
          upstreamModelId: "Entry-A",
        }),
      ]),
    );
    const notParticipating = screen.getByText("未参与");
    expect(notParticipating).toBeInTheDocument();
    expect(notParticipating).toHaveAttribute(
      "title",
      "该目标未启用，不参与路由。",
    );
  });

  it("flags a disabled Model Target row as 未参与 too", () => {
    renderCell(
      entryModelListItem([modelTargetRow(9, 0, { isEnabled: false })]),
    );
    expect(screen.getByText("未参与")).toBeInTheDocument();
  });

  it("shows all three rows when folding would save nothing", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(23, 1, {
          endpointName: "ep-b",
          upstreamModelId: "Entry-A",
        }),
        modelTargetRow(7, 0, { summaryModelId: "child-summary" }),
        terminalTargetRow(31, 2, {
          endpointName: "ep-c",
          upstreamModelId: "Entry-A",
        }),
      ]),
    );
    // Ordered rows: the Model Target (position 0) first, then position 1.
    expect(screen.getByText("child-summary")).toBeInTheDocument();
    expect(screen.getByTitle("ep-b")).toBeInTheDocument();
    // 余量恰为 1 时折叠零节省，第三条直接显示，尾行不出现。
    expect(screen.getByTitle("ep-c")).toBeInTheDocument();
    expect(screen.queryByText(/还有 .* 项，见详情/)).not.toBeInTheDocument();
  });

  it("renders a failed routing summary as an error reason instead of exit rows", () => {
    const model = entryModelListItem([terminalTargetRow(11, 0)]);
    model.routing_summary = null;
    renderCell(model);
    const dash = screen.getByText("—");
    expect(dash.closest("[data-slot=missing-value]")).not.toBeNull();
    expect(
      screen.getByText("路由摘要读取失败，无法展示使用的服务。"),
    ).toHaveClass("sr-only");
    expect(screen.queryByText(/启用 \//)).not.toBeInTheDocument();
  });

  it("renders zero targets as the 尚未连接服务 failing state", () => {
    const model = entryModelListItem([]);
    model.routing_summary = routingSummary({
      enabled_access_target_count: 0,
      total_access_target_count: 0,
    });
    renderCell(model);
    const badge = screen.getByText("尚未连接服务");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute(
      "title",
      "该模型没有任何服务或转发配置，请求无法路由。",
    );
  });
});
