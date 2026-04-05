import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { RuleDialog } from "../dialogs/RuleDialog";

describe("RuleDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("submits through a real form and exposes stable field names", () => {
    const handleSaveRule = vi.fn().mockResolvedValue(undefined);
    render(
      <LocaleProvider>
        <TooltipProvider>
          <RuleDialog
            ruleDialogOpen={true}
            setRuleDialogOpen={vi.fn()}
            editingRule={null}
            ruleForm={{ enabled: true, match_type: "exact", name: "Authorization", pattern: "authorization" }}
            setRuleForm={vi.fn()}
            handleSaveRule={handleSaveRule}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getByLabelText("Name")).toHaveAttribute("name", "name");
    expect(screen.getByLabelText("Name")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("Pattern")).toHaveAttribute("name", "pattern");
    expect(screen.getByLabelText("Pattern")).toHaveAttribute("autocomplete", "off");
    expect(document.querySelector('input[type="hidden"][name="match_type"]')).toHaveValue("exact");

    const form = screen.getByRole("button", { name: "Save Rule" }).closest("form");
    expect(form).not.toBeNull();

    fireEvent.submit(form!);

    expect(handleSaveRule).toHaveBeenCalledTimes(1);
  });
});
