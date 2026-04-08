import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { HeaderBlocklistRule, Vendor } from "@/lib/types";
import { AuditConfigurationRulesPanel } from "../sections/AuditConfigurationRulesPanel";
import { AuditConfigurationSection } from "../sections/AuditConfigurationSection";

interface UserAgentClientRuleShape {
  id: number;
  name: string;
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

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

vi.mock("../sections/AuditConfigurationUserAgentClientRulesCard", () => ({
  AuditConfigurationUserAgentClientRulesCard: ({
    customRules,
    systemRules,
    handleToggleRule,
    openAddRuleDialog,
    openEditRuleDialog,
    setDeleteRuleConfirm,
    setSystemRulesOpen,
    setUserRulesOpen,
  }: {
    customRules: UserAgentClientRuleShape[];
    systemRules: UserAgentClientRuleShape[];
    handleToggleRule: (rule: UserAgentClientRuleShape, checked: boolean) => Promise<void>;
    openAddRuleDialog: () => void;
    openEditRuleDialog: (rule: UserAgentClientRuleShape) => void;
    setDeleteRuleConfirm: (rule: UserAgentClientRuleShape | null) => void;
    setSystemRulesOpen: (open: boolean) => void;
    setUserRulesOpen: (open: boolean) => void;
  }) => (
    <div>
      <div>{`user-agent-client-rules-card:${systemRules.length}:${customRules.length}`}</div>
      <button type="button" onClick={openAddRuleDialog}>
        add-user-agent-client-rule
      </button>
      <button type="button" onClick={() => setSystemRulesOpen(true)}>
        open-user-agent-system-rules
      </button>
      <button type="button" onClick={() => setUserRulesOpen(true)}>
        open-user-agent-custom-rules
      </button>
      <button type="button" onClick={() => void handleToggleRule(customRules[0], false)}>
        toggle-user-agent-custom-rule
      </button>
      <button type="button" onClick={() => openEditRuleDialog(customRules[0])}>
        edit-user-agent-custom-rule
      </button>
      <button type="button" onClick={() => setDeleteRuleConfirm(customRules[0])}>
        delete-user-agent-custom-rule
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

const userAgentClientSystemRule: UserAgentClientRuleShape = {
  id: 11,
  name: "Claude Code",
  enabled: true,
  is_system: true,
  pattern: "Claude\\sCode",
  created_at: "",
  updated_at: "",
};

const userAgentClientCustomRule: UserAgentClientRuleShape = {
  id: 12,
  name: "Codex",
  enabled: true,
  is_system: false,
  pattern: "Codex",
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
  it("renders vendor audit defaults plus separate header-blocklist and user-agent client rule cards", () => {
    const toggleAudit = vi.fn().mockResolvedValue(undefined);
    const toggleBodies = vi.fn().mockResolvedValue(undefined);
    const setSystemRulesOpen = vi.fn();
    const setUserRulesOpen = vi.fn();
    const handleToggleRule = vi.fn().mockResolvedValue(undefined);
    const openAddRuleDialog = vi.fn();
    const openEditRuleDialog = vi.fn();
    const setDeleteRuleConfirm = vi.fn();
    const setUserAgentClientSystemRulesOpen = vi.fn();
    const setUserAgentClientUserRulesOpen = vi.fn();
    const handleToggleUserAgentClientRule = vi.fn().mockResolvedValue(undefined);
    const openAddUserAgentClientRuleDialog = vi.fn();
    const openEditUserAgentClientRuleDialog = vi.fn();
    const setDeleteUserAgentClientRuleConfirm = vi.fn();

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
          loadingUserAgentClientRules={false}
          userAgentClientSystemRulesOpen={false}
          setUserAgentClientSystemRulesOpen={setUserAgentClientSystemRulesOpen}
          userAgentClientSystemRules={[userAgentClientSystemRule]}
          userAgentClientUserRulesOpen={false}
          setUserAgentClientUserRulesOpen={setUserAgentClientUserRulesOpen}
          userAgentClientCustomRules={[userAgentClientCustomRule]}
          handleToggleUserAgentClientRule={handleToggleUserAgentClientRule}
          openAddUserAgentClientRuleDialog={openAddUserAgentClientRuleDialog}
          openEditUserAgentClientRuleDialog={openEditUserAgentClientRuleDialog}
          setDeleteUserAgentClientRuleConfirm={setDeleteUserAgentClientRuleConfirm}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Configure vendor-level audit capture and privacy defaults.")).toBeInTheDocument();
    expect(screen.getByText("OpenAI")).toBeInTheDocument();
    expect(screen.getByLabelText("Vendor icon OpenAI")).toBeInTheDocument();
    expect(screen.getByText("header-blocklist-card:1:1")).toBeInTheDocument();
    expect(screen.getByText("user-agent-client-rules-card:1:1")).toBeInTheDocument();

    const [auditSwitch, bodiesSwitch] = screen.getAllByRole("switch");

    fireEvent.click(auditSwitch);
    fireEvent.click(bodiesSwitch);
    fireEvent.click(screen.getByRole("button", { name: "add-blocklist-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "open-system-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "open-user-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "toggle-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "edit-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "delete-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "add-user-agent-client-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "open-user-agent-system-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "open-user-agent-custom-rules" }));
    fireEvent.click(screen.getByRole("button", { name: "toggle-user-agent-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "edit-user-agent-custom-rule" }));
    fireEvent.click(screen.getByRole("button", { name: "delete-user-agent-custom-rule" }));

    expect(toggleAudit).toHaveBeenCalledWith(7, false);
    expect(toggleBodies).toHaveBeenCalledWith(7, true);
    expect(openAddRuleDialog).toHaveBeenCalledTimes(1);
    expect(setSystemRulesOpen).toHaveBeenCalledWith(true);
    expect(setUserRulesOpen).toHaveBeenCalledWith(true);
    expect(handleToggleRule).toHaveBeenCalledWith(customRule, false);
    expect(openEditRuleDialog).toHaveBeenCalledWith(customRule);
    expect(setDeleteRuleConfirm).toHaveBeenCalledWith(customRule);
    expect(openAddUserAgentClientRuleDialog).toHaveBeenCalledTimes(1);
    expect(setUserAgentClientSystemRulesOpen).toHaveBeenCalledWith(true);
    expect(setUserAgentClientUserRulesOpen).toHaveBeenCalledWith(true);
    expect(handleToggleUserAgentClientRule).toHaveBeenCalledWith(userAgentClientCustomRule, false);
    expect(openEditUserAgentClientRuleDialog).toHaveBeenCalledWith(userAgentClientCustomRule);
    expect(setDeleteUserAgentClientRuleConfirm).toHaveBeenCalledWith(userAgentClientCustomRule);
  });
});
