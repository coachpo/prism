import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ObserveCoverage } from "@/lib/api/observability";
import { ObserveScopedCoverageWarnings } from "./ObserveScopedCoverageWarnings";

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({ format: (value: string) => value }),
}));

function coverage(
  complete: boolean,
  gaps: ObserveCoverage["gaps"] = [],
): ObserveCoverage {
  return {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    retention_from_time: null,
    source: "raw",
    complete,
    gaps,
  };
}

function renderWarnings(
  usageCoverage: ObserveCoverage,
  requestCoverage: ObserveCoverage,
) {
  return render(
    <LocaleProvider>
      <ObserveScopedCoverageWarnings
        scope="final_execution"
        usageCoverage={usageCoverage}
        requestCoverage={requestCoverage}
      />
    </LocaleProvider>,
  );
}

describe("Observe scoped coverage warnings", () => {
  it("keeps a usage-event gap separate from complete final-attempt latency evidence", () => {
    renderWarnings(
      coverage(false, [
        {
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-08T01:00:00Z",
          reason: "retention_deleted",
        },
      ]),
      coverage(true),
    );

    expect(screen.getByText("最终承载用量事件覆盖受限")).toBeInTheDocument();
    expect(screen.queryByText("最终尝试延迟覆盖受限")).not.toBeInTheDocument();
    expect(
      screen.getByText(/2026-08-08T00:00:00Z 至 2026-08-08T01:00:00Z/),
    ).toHaveTextContent("已超出当前保留范围");
  });

  it("limits a request-log gap to final-attempt latency", () => {
    renderWarnings(
      coverage(true),
      coverage(false, [
        {
          from_time: "2026-08-08T03:00:00Z",
          to_time: "2026-08-08T04:00:00Z",
          reason: "actual_coverage_unavailable",
        },
      ]),
    );

    expect(screen.getByText("最终尝试延迟覆盖受限")).toBeInTheDocument();
    expect(
      screen.getByText(/此缺口只限制最终尝试延迟样本/),
    ).toBeInTheDocument();
    expect(screen.queryByText("最终承载用量事件覆盖受限")).not.toBeInTheDocument();
    expect(screen.getByText(/owner 尚无可用覆盖证据/)).toBeInTheDocument();
  });

  it("renders two independently labelled warnings when both datasets have gaps", () => {
    renderWarnings(coverage(false), coverage(false));

    expect(screen.getByTestId("final_usage-coverage-warning")).toBeInTheDocument();
    expect(screen.getByTestId("final_latency-coverage-warning")).toBeInTheDocument();
    expect(screen.queryByText("已知缺口")).not.toBeInTheDocument();
  });
});
