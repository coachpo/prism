import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { UserAgentClientRuleCreate } from "@/lib/types";
import { RuleDialog } from "../dialogs/RuleDialog";
import { UserAgentClientRuleDialog } from "../dialogs/UserAgentClientRuleDialog";

function UserAgentClientRuleDialogHarness({
  handleSaveRule,
}: {
  handleSaveRule: () => Promise<void>;
}) {
  const [ruleForm, setRuleForm] = useState<UserAgentClientRuleCreate>({
    enabled: true,
    name: "",
    pattern: "",
  });

  return (
    <UserAgentClientRuleDialog
      ruleDialogOpen={true}
      setRuleDialogOpen={vi.fn()}
      editingRule={null}
      ruleForm={ruleForm}
      setRuleForm={setRuleForm}
      handleSaveRule={handleSaveRule}
    />
  );
}

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

  it("submits invalid regex patterns to the backend instead of blocking in the browser", () => {
    const handleSaveRule = vi.fn().mockResolvedValue(undefined);

    render(
      <LocaleProvider>
        <TooltipProvider>
          <UserAgentClientRuleDialogHarness handleSaveRule={handleSaveRule} />
        </TooltipProvider>
      </LocaleProvider>,
    );

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Codex CLI" },
    });
    fireEvent.change(screen.getByLabelText("Regex pattern"), {
      target: { value: "[Codex" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    expect(handleSaveRule).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Enter a valid regular expression.")).not.toBeInTheDocument();
  });
});
