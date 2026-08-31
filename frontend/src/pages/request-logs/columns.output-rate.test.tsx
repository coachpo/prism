import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import type { RequestLogListItem } from "@/lib/types";
import { getColumns } from "./columns";

afterEach(cleanup);

function renderTokenRate(row: Partial<RequestLogListItem>) {
  const column = getColumns().find((candidate) => candidate.key === "token_rate");
  if (!column) throw new Error("token_rate column missing");
  render(<>{column.render(row as RequestLogListItem, () => "")}</>);
}

describe("request-log output-rate column", () => {
  it("does not resurrect the GLM 53k artifact from raw timing fields", () => {
    renderTokenRate({
      output_tokens: 53,
      ttft_ms: 23_495,
      completion_duration_ms: 23_496,
      output_rate_tps: null,
      output_rate_state: "unmeasurable",
      output_rate_reason: "unmeasurable_output_span_below_threshold",
    });

    expect(screen.queryByText("53,000.0 tok/s")).not.toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("keeps a measured zero as a real numeric value", () => {
    renderTokenRate({
      output_rate_tps: 0,
      output_rate_state: "measured",
      output_rate_reason: null,
    });

    expect(screen.getByText("0.0 tok/s")).toBeInTheDocument();
  });
});
