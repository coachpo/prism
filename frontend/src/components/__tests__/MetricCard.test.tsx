import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MetricCard } from "@/components/MetricCard";
import { Badge } from "@/components/ui/badge";

describe("MetricCard", () => {
  it("keeps label badges wrapped inside the card shell", () => {
    const { container } = render(
      <MetricCard
        label={
          <>
            <span>Monitoring health</span>
            <Badge variant="outline">Healthy</Badge>
          </>
        }
        value="42"
        detail="Latest sample"
      />,
    );

    const card = container.querySelector('[data-slot="metric-card"]');
    const label = screen.getByText("Monitoring health").closest('[data-slot="metric-label"]');
    const content = screen.getByText("Monitoring health").closest('[data-slot="metric-card"]')?.querySelector('[data-slot="card-content"]');

    expect(card).toHaveClass("overflow-hidden");
    expect(content).toHaveClass("overflow-hidden");
    expect(label).toHaveClass("flex", "min-w-0", "flex-wrap", "items-center", "gap-2");
    expect(label).toHaveClass("overflow-hidden");
  });

  it("keeps long values and trend content inside the card content row", () => {
    render(
      <MetricCard
        detail="Latest sample"
        label="Monitoring health"
        trend={{ value: "+12.5% over the previous interval", positive: true }}
        value="extremely-long-metric-value-that-should-wrap-without-pushing-the-card-wider"
      />,
    );

    const value = screen.getByText(/extremely-long-metric-value/).closest('[data-slot="metric-value"]');
    const trend = screen.getByText("+12.5% over the previous interval");

    expect(value).toHaveClass("min-w-0", "break-words");
    expect(trend).toHaveClass("max-w-full", "break-words");
  });
});
