import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { HeaderBlocklistRule, Vendor } from "@/lib/types";
import { AuditConfigurationRulesPanel } from "../sections/AuditConfigurationRulesPanel";
import { AuditConfigurationSection } from "../sections/AuditConfigurationSection";

vi.mock("../sections/AuditConfigurationHeaderBlocklistCard", () => ({
  AuditConfigurationHeaderBlocklistCard: ({
    customRules,
    systemRules,
    handleToggleRule,
    openAddRuleDialog,
    openEditRuleDialog,
    setDeleteRuleConfirm,
    setSystemRulesOpen,
    setUserRulesOpen,
  }: {
    customRules: HeaderBlocklistRule[];
    systemRules: HeaderBlocklistRule[];
    handleToggleRule: (rule: HeaderBlocklistRule, checked: boolean) => Promise<void>;
    openAddRuleDialog: () => void;
    openEditRuleDialog: (rule: HeaderBlocklistRule) => void;
    setDeleteRuleConfirm: (rule: HeaderBlocklistRule | null) => void;
    setSystemRulesOpen: (open: boolean) => void;
    setUserRulesOpen: (open: boolean) => void;
  }) => (
    <div>
      <div>{`header-blocklist-card:${systemRules.length}:${customRules.length}`}</div>
      <button type="button" onClick={openAddRuleDialog}>
        add-blocklist-rule
      </button>
      <button type="button" onClick={() => setSystemRulesOpen(true)}>
        open-system-rules
      </button>
      <button type="button" onClick={() => setUserRulesOpen(true)}>
        open-user-rules
      </button>
      <button type="button" onClick={() => void handleToggleRule(customRules[0], false)}>
        toggle-custom-rule
      </button>
      <button type="button" onClick={() => openEditRuleDialog(customRules[0])}>
        edit-custom-rule
      </button>
      <button type="button" onClick={() => setDeleteRuleConfirm(customRules[0])}>
        delete-custom-rule
      </button>
    </div>
  ),
}));

const vendor: Vendor = {
  id: 7,
  key: "openai",
  name: "OpenAI",
  description: null,
  icon_key: "openai",
  audit_enabled: true,
  audit_capture_bodies: false,
  created_at: "",
  updated_at: "",
};

const systemRule: HeaderBlocklistRule = {
  id: 1,
  name: "authorization",
  enabled: true,
  is_system: true,
  match_type: "exact",
  pattern: "authorization",
  created_at: "",
  updated_at: "",
};

const customRule: HeaderBlocklistRule = {
  id: 2,
  name: "x-trace-id",
  enabled: true,
  is_system: false,
  match_type: "exact",
  pattern: "x-trace-id",
  created_at: "",
  updated_at: "",
};

describe("AuditConfigurationRulesPanel", () => {
  it("renders system and custom rule groups with the add-rule action", () => {
    render(
      <LocaleProvider>
        <AuditConfigurationRulesPanel
          customRules={[]}
          loadingRules={false}
          onDeleteRule={vi.fn()}
          onEditRule={vi.fn()}
          onOpenAddRuleDialog={vi.fn()}
          onOpenChangeSystemRules={vi.fn()}
          onOpenChangeUserRules={vi.fn()}
          onToggleRule={vi.fn()}
          systemRules={[systemRule]}
          systemRulesOpen={true}
          userRulesOpen={true}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("System rules (locked)")).toBeInTheDocument();
    expect(screen.getByText("Custom rules")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add Rule" })).toBeInTheDocument();
  });
});

describe("AuditConfigurationSection", () => {
  it("renders vendor audit defaults and keeps the header-blocklist actions wired", () => {
    const toggleAudit = vi.fn().mockResolvedValue(undefined);
    const toggleBodies = vi.fn().mockResolvedValue(undefined);
    const setSystemRulesOpen = vi.fn();
    const setUserRulesOpen = vi.fn();
    const handleToggleRule = vi.fn().mockResolvedValue(undefined);
    const openAddRuleDialog = vi.fn();
    const openEditRuleDialog = vi.fn();
    const setDeleteRuleConfirm = vi.fn();

    render(
      <LocaleProvider>
        <AuditConfigurationSection
          auditConfigurationRef={createRef<HTMLDivElement>()}
          isAuditConfigurationFocused={false}
          vendors={[vendor]}
          toggleAudit={toggleAudit}
          toggleBodies={toggleBodies}
          loadingRules={false}
          systemRulesOpen={false}
          setSystemRulesOpen={setSystemRulesOpen}
          systemRules={[systemRule]}
          userRulesOpen={false}
          setUserRulesOpen={setUserRulesOpen}
          customRules={[customRule]}
          handleToggleRule={handleToggleRule}
          openAddRuleDialog={openAddRuleDialog}
          openEditRuleDialog={openEditRuleDialog}
          setDeleteRuleConfirm={setDeleteRuleConfirm}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Configure vendor-level audit capture and privacy defaults.")).toBeInTheDocument();
    expect(screen.getByText("OpenAI")).toBeInTheDocument();
    expect(screen.getByLabelText("Vendor icon OpenAI")).toBeInTheDocument();
    expect(screen.getByText("header-blocklist-card:1:1")).toBeInTheDocument();

    const [auditSwitch, bodiesSwitch] = screen.getAllByRole("switch");

    fireEvent.click(auditSwitch);
    fireEvent.click(bodiesSwitch);
    fireEvent.click(screen.getByRole("button", { name: "add-blocklist-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "open-system-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "open-user-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "toggle-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "edit-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "delete-custom-rule" }));

    expect(toggleAudit).toHaveBeenCalledWith(7, false);
    expect(toggleBodies).toHaveBeenCalledWith(7, true);
    expect(openAddRuleDialog).toHaveBeenCalledTimes(1);
    expect(setSystemRulesOpen).toHaveBeenCalledWith(true);
    expect(setUserRulesOpen).toHaveBeenCalledWith(true);
    expect(handleToggleRule).toHaveBeenCalledWith(customRule, false);
    expect(openEditRuleDialog).toHaveBeenCalledWith(customRule);
    expect(setDeleteRuleConfirm).toHaveBeenCalledWith(customRule);
  });
});
