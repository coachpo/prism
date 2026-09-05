// Honesty Contract, route readiness card: a failed diagnostics read must own
// the panel area instead of removing it. Before this, "not read yet", "read
// failed" and "the backend has no diagnostics" were all one null and the panel
// simply vanished, so a broken fetch looked exactly like a clean empty result.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { ModelConfig } from "@/lib/types";
import { RouteReadinessCard } from "@/pages/model-detail/RouteReadinessCard";

const model = {
  id: 1,
  profile_id: 1,
  api_family: "openai",
  model_id: "gpt-test",
  display_name: "GPT Test",
  is_enabled: true,
} as unknown as ModelConfig;

function renderCard(props: Partial<React.ComponentProps<typeof RouteReadinessCard>>) {
  return render(
    <LocaleProvider>
      <TooltipProvider>
        <RouteReadinessCard
          model={model}
          diagnosticsView={{ kind: "idle" }}
          onRetryDiagnostics={() => {}}
          {...props}
        />
      </TooltipProvider>
    </LocaleProvider>,
  );
}

describe("RouteReadinessCard diagnostics states", () => {
  it("renders a failure surface instead of dropping the panel", () => {
    renderCard({ diagnosticsView: { kind: "error", message: "boom" } });
    expect(screen.getByTestId("route-readiness-diagnostics-error")).toBeInTheDocument();
  });

  it("keeps a pending read distinct from a failure", () => {
    renderCard({ diagnosticsView: { kind: "loading" } });
    expect(screen.getByTestId("route-readiness-diagnostics-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("route-readiness-diagnostics-error")).not.toBeInTheDocument();
  });

  it("invokes the retry handler so the caller can issue a second request", async () => {
    const onRetryDiagnostics = vi.fn();
    renderCard({ diagnosticsView: { kind: "error", message: "boom" }, onRetryDiagnostics });
    await userEvent.click(screen.getByTestId("route-readiness-diagnostics-error").querySelector("button")!);
    expect(onRetryDiagnostics).toHaveBeenCalledTimes(1);
  });
});
