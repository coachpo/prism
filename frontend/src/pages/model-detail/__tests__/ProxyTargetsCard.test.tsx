import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ProxyTargetsCard } from "../ProxyTargetsCard";

describe("ProxyTargetsCard", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    HTMLElement.prototype.scrollIntoView = vi.fn();
  });

  it("requires an explicit remaining-target selection before adding and saves edited target order", () => {
    const onSave = vi.fn();

    render(
      <LocaleProvider>
        <ProxyTargetsCard
          availableTargets={[
            { modelId: "claude-sonnet-4-5-20250929", label: "Claude Sonnet 4.5 (20250929)" },
            { modelId: "claude-sonnet-4-5-20250701", label: "Claude Sonnet 4.5 (20250701)" },
            { modelId: "claude-sonnet-4-5-20250301", label: "Claude Sonnet 4.5 (20250301)" },
          ]}
          proxyTargets={[
            { target_model_id: "claude-sonnet-4-5-20250929", position: 0 },
            { target_model_id: "claude-sonnet-4-5-20250701", position: 1 },
          ]}
          saving={false}
          onSave={onSave}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Ordered priority routing")).toBeInTheDocument();
    expect(
      screen.getByText("Requests try these native targets in order and stop at the first available target."),
    ).toBeInTheDocument();

    const addTargetButton = screen.getByRole("button", { name: "Add Target" });
    expect(addTargetButton).toBeDisabled();

    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(screen.getByText("Claude Sonnet 4.5 (20250301)"));

    expect(addTargetButton).not.toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Add Target" }));
    fireEvent.click(screen.getByRole("button", { name: "Move target claude-sonnet-4-5-20250301 up" }));
    fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));

    expect(onSave).toHaveBeenCalledWith([
      { target_model_id: "claude-sonnet-4-5-20250929", position: 0 },
      { target_model_id: "claude-sonnet-4-5-20250301", position: 1 },
      { target_model_id: "claude-sonnet-4-5-20250701", position: 2 },
    ]);
  });
});
