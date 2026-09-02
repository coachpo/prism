// ModelExitMappingCell: the models-list exit-mapping cell rendering. The
// projection logic lives in modelExitMapping.test.ts; this suite pins what the
// operator actually sees per the DESIGN.md honesty contract: real endpoint +
// upstream identity for Terminal Targets, the logical id for Model Targets,
// reasoned `—` for missing evidence, and textual (never color-only)
// 已解耦/未参与 states.
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
    // The entry id is never substituted for the upstream identity.
    expect(screen.queryByText(ENTRY_MODEL_ID)).not.toBeInTheDocument();
  });

  it("marks a case-sensitive decoupled upstream identity with the full reason", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, {
          endpointName: "OpenAI Primary",
          upstreamModelId: "entry-a",
        }),
      ]),
    );
    const decoupled = screen.getByText("已解耦");
    expect(decoupled).toBeInTheDocument();
    expect(decoupled).toHaveAttribute(
      "title",
      `上游模型 ID「entry-a」与入口模型 ID「${ENTRY_MODEL_ID}」精确比较不一致（区分大小写）。`,
    );
  });

  it("never marks a matching-case upstream identity as decoupled", () => {
    renderCell(
      entryModelListItem([
        terminalTargetRow(11, 0, { upstreamModelId: "Entry-A" }),
      ]),
    );
    expect(screen.queryByText("已解耦")).not.toBeInTheDocument();
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
      "模型目标 child-summary：逻辑目标；实际供应商出口由它解析到的终端目标持有。",
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
        "该终端目标没有可读的上游模型 ID 证据；不会用入口模型 ID 代填。",
      ),
    ).toHaveClass("sr-only");
    expect(
      screen.getByText("该终端目标行没有端点引用，因此无法显示端点。"),
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

  it("shows the first two rows in (position, id) order and links the remainder to detail", () => {
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
    // The third row never renders in the cell; the remainder line points at
    // the detail page instead.
    expect(screen.queryByTitle("ep-c")).not.toBeInTheDocument();
    expect(screen.getByText("还有 1 项，见详情")).toBeInTheDocument();
  });

  it("renders a failed routing summary as an error reason instead of exit rows", () => {
    const model = entryModelListItem([terminalTargetRow(11, 0)]);
    model.routing_summary = null;
    renderCell(model);
    const dash = screen.getByText("—");
    expect(dash.closest("[data-slot=missing-value]")).not.toBeNull();
    expect(
      screen.getByText("路由摘要读取失败，无法展示出口映射。"),
    ).toHaveClass("sr-only");
    expect(screen.queryByText(/启用 \//)).not.toBeInTheDocument();
  });

  it("renders zero targets as the 需要目标 failing state", () => {
    const model = entryModelListItem([]);
    model.routing_summary = routingSummary({
      enabled_access_target_count: 0,
      total_access_target_count: 0,
    });
    renderCell(model);
    const badge = screen.getByText("需要目标");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute(
      "title",
      "该模型没有任何访问目标，请求无法路由。",
    );
  });
});
