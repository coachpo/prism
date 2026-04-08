import type { Dispatch, SetStateAction } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { HeaderBlocklistRule, Vendor } from "@/lib/types";
import { useAuditConfigurationData } from "../useAuditConfigurationData";

interface UserAgentClientRuleShape {
  id: number;
  name: string;
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

interface UserAgentClientRuleFormShape {
  name: string;
  pattern: string;
  enabled: boolean;
}

type UserAgentClientAuditConfigurationSlice = ReturnType<typeof useAuditConfigurationData> & {
  deleteUserAgentClientRuleConfirm: UserAgentClientRuleShape | null;
  displayedDeleteUserAgentClientRuleConfirm: UserAgentClientRuleShape | null;
  editingUserAgentClientRule: UserAgentClientRuleShape | null;
  handleDeleteUserAgentClientRule: () => Promise<void>;
  handleSaveUserAgentClientRule: () => Promise<void>;
  handleToggleUserAgentClientRule: (
    rule: UserAgentClientRuleShape,
    checked: boolean,
  ) => Promise<void>;
  openAddUserAgentClientRuleDialog: () => void;
  openEditUserAgentClientRuleDialog: (rule: UserAgentClientRuleShape) => void;
  setDeleteUserAgentClientRuleConfirm: (rule: UserAgentClientRuleShape | null) => void;
  setUserAgentClientRuleForm: Dispatch<SetStateAction<UserAgentClientRuleFormShape>>;
  userAgentClientCustomRules: UserAgentClientRuleShape[];
  userAgentClientRuleDialogOpen: boolean;
  userAgentClientRuleForm: UserAgentClientRuleFormShape;
  userAgentClientSystemRules: UserAgentClientRuleShape[];
};

const api = vi.hoisted(() => ({
  config: {
    headerBlocklistRules: {
      create: vi.fn(),
      delete: vi.fn(),
      list: vi.fn(),
      update: vi.fn(),
    },
    userAgentClientRules: {
      create: vi.fn(),
      delete: vi.fn(),
      list: vi.fn(),
      update: vi.fn(),
    },
  },
}));

const referenceData = vi.hoisted(() => ({
  getSharedVendors: vi.fn(),
  setSharedVendors: vi.fn(),
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("@/lib/referenceData", () => ({
  getSharedVendors: referenceData.getSharedVendors,
  setSharedVendors: referenceData.setSharedVendors,
}));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: "openai",
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function buildRule(overrides: Partial<HeaderBlocklistRule> = {}): HeaderBlocklistRule {
  return {
    id: 2,
    name: "authorization",
    enabled: true,
    is_system: false,
    match_type: "exact",
    pattern: "authorization",
    created_at: "2026-03-30T09:00:00Z",
    updated_at: "2026-03-30T10:00:00Z",
    ...overrides,
  };
}

function buildUserAgentClientRule(
  overrides: Partial<UserAgentClientRuleShape> = {},
): UserAgentClientRuleShape {
  return {
    id: 12,
    name: "Codex",
    enabled: true,
    is_system: false,
    pattern: "Codex",
    created_at: "2026-04-08T09:00:00Z",
    updated_at: "2026-04-08T10:00:00Z",
    ...overrides,
  };
}

describe("useAuditConfigurationData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    referenceData.getSharedVendors.mockResolvedValue([buildVendor()]);
    api.config.headerBlocklistRules.list.mockResolvedValue([]);
    api.config.userAgentClientRules.list.mockResolvedValue([]);
  });

  it("keeps the create rule draft stable while closing after a successful save", async () => {
    const created = buildRule({ id: 5, name: "x-trace-id", pattern: "x-trace-id" });
    api.config.headerBlocklistRules.create.mockResolvedValue(created);

    const { result } = renderHook(() => useAuditConfigurationData({ revision: 3 }));

    await waitFor(() => {
      expect(result.current.loadingRules).toBe(false);
    });

    act(() => {
      result.current.openAddRuleDialog();
      result.current.setRuleForm({
        enabled: true,
        match_type: "exact",
        name: "x-trace-id",
        pattern: "x-trace-id",
      });
    });

    await act(async () => {
      await result.current.handleSaveRule();
    });

    expect(api.config.headerBlocklistRules.create).toHaveBeenCalledWith({
      enabled: true,
      match_type: "exact",
      name: "x-trace-id",
      pattern: "x-trace-id",
    });
    expect(result.current.ruleDialogOpen).toBe(false);
    expect(result.current.ruleForm).toEqual({
      enabled: true,
      match_type: "exact",
      name: "x-trace-id",
      pattern: "x-trace-id",
    });
    expect(toast.success).toHaveBeenCalledWith("Rule created successfully");
  });

  it("keeps the edit rule snapshot stable while closing after a successful save", async () => {
    const existing = buildRule({ id: 7, name: "authorization" });
    const updated = buildRule({
      id: 7,
      name: "x-request-id",
      pattern: "x-request-id",
      updated_at: "2026-03-30T11:00:00Z",
    });
    api.config.headerBlocklistRules.list.mockResolvedValue([existing]);
    api.config.headerBlocklistRules.update.mockResolvedValue(updated);

    const { result } = renderHook(() => useAuditConfigurationData({ revision: 3 }));

    await waitFor(() => {
      expect(result.current.loadingRules).toBe(false);
      expect(result.current.customRules[0]?.id).toBe(7);
    });

    act(() => {
      result.current.openEditRuleDialog(existing);
      result.current.setRuleForm({
        enabled: true,
        match_type: "exact",
        name: "x-request-id",
        pattern: "x-request-id",
      });
    });

    await act(async () => {
      await result.current.handleSaveRule();
    });

    expect(api.config.headerBlocklistRules.update).toHaveBeenCalledWith(7, {
      enabled: true,
      match_type: "exact",
      name: "x-request-id",
      pattern: "x-request-id",
    });
    expect(result.current.ruleDialogOpen).toBe(false);
    expect(result.current.editingRule?.id).toBe(7);
    expect(result.current.ruleForm).toEqual({
      enabled: true,
      match_type: "exact",
      name: "x-request-id",
      pattern: "x-request-id",
    });
    expect(toast.success).toHaveBeenCalledWith("Rule updated successfully");
  });

  it("closes the delete rule dialog without dropping the displayed rule snapshot", async () => {
    const existing = buildRule({ id: 9, name: "x-trace-id", pattern: "x-trace-id" });
    api.config.headerBlocklistRules.list.mockResolvedValue([existing]);
    api.config.headerBlocklistRules.delete.mockResolvedValue(undefined);

    const { result } = renderHook(() => useAuditConfigurationData({ revision: 3 }));

    await waitFor(() => {
      expect(result.current.loadingRules).toBe(false);
      expect(result.current.customRules[0]?.id).toBe(9);
    });

    act(() => {
      result.current.setDeleteRuleConfirm(existing);
    });

    await act(async () => {
      await result.current.handleDeleteRule();
    });

    expect(result.current.customRules).toEqual([]);
    expect(result.current.deleteRuleDialogOpen).toBe(false);
    expect(result.current.deleteRuleConfirm).toBeNull();
    expect(result.current.displayedDeleteRuleConfirm).toMatchObject({
      id: 9,
      name: "x-trace-id",
      pattern: "x-trace-id",
    });
    expect(toast.success).toHaveBeenCalledWith("Rule deleted successfully");
  });

  it("fetches and mutates dedicated user-agent client rules without reusing header blocklist state", async () => {
    const systemRule = buildUserAgentClientRule({
      id: 31,
      is_system: true,
      name: "Claude Code",
      pattern: "Claude\\sCode",
    });
    const customRule = buildUserAgentClientRule({
      id: 32,
      name: "Codex CLI",
      pattern: "Codex",
    });
    const createdRule = buildUserAgentClientRule({
      id: 33,
      name: "Gemini CLI",
      pattern: "Gemini",
    });
    const updatedRule = buildUserAgentClientRule({
      id: 32,
      name: "Codex Agent",
      pattern: "Codex-Agent",
      enabled: false,
    });

    api.config.userAgentClientRules.list.mockResolvedValue([systemRule, customRule]);
    api.config.userAgentClientRules.create.mockResolvedValue(createdRule);
    api.config.userAgentClientRules.update.mockResolvedValue(updatedRule);
    api.config.userAgentClientRules.delete.mockResolvedValue(undefined);

    const { result } = renderHook(() => useAuditConfigurationData({ revision: 3 }));
    const getAuditData = () => result.current as UserAgentClientAuditConfigurationSlice;

    await waitFor(() => {
      expect(result.current.loadingRules).toBe(false);
      expect(api.config.userAgentClientRules.list).toHaveBeenCalledWith(true);
      expect(getAuditData().userAgentClientSystemRules).toHaveLength(1);
      expect(getAuditData().userAgentClientCustomRules).toHaveLength(1);
    });

    await act(async () => {
      await getAuditData().handleToggleUserAgentClientRule(customRule, false);
    });

    expect(api.config.userAgentClientRules.update).toHaveBeenCalledWith(32, { enabled: false });

    act(() => {
      getAuditData().openAddUserAgentClientRuleDialog();
      getAuditData().setUserAgentClientRuleForm({
        enabled: true,
        name: "Gemini CLI",
        pattern: "Gemini",
      });
    });

    await act(async () => {
      await getAuditData().handleSaveUserAgentClientRule();
    });

    expect(api.config.userAgentClientRules.create).toHaveBeenCalledWith({
      enabled: true,
      name: "Gemini CLI",
      pattern: "Gemini",
    });
    expect(getAuditData().userAgentClientRuleDialogOpen).toBe(false);
    expect(getAuditData().userAgentClientRuleForm).toEqual({
      enabled: true,
      name: "Gemini CLI",
      pattern: "Gemini",
    });

    act(() => {
      getAuditData().openEditUserAgentClientRuleDialog(customRule);
      getAuditData().setUserAgentClientRuleForm({
        enabled: false,
        name: "Codex Agent",
        pattern: "Codex-Agent",
      });
    });

    await act(async () => {
      await getAuditData().handleSaveUserAgentClientRule();
    });

    expect(api.config.userAgentClientRules.update).toHaveBeenCalledWith(32, {
      enabled: false,
      name: "Codex Agent",
      pattern: "Codex-Agent",
    });
    expect(getAuditData().editingUserAgentClientRule?.id).toBe(32);

    act(() => {
      getAuditData().setDeleteUserAgentClientRuleConfirm(updatedRule);
    });

    await act(async () => {
      await getAuditData().handleDeleteUserAgentClientRule();
    });

    expect(api.config.userAgentClientRules.delete).toHaveBeenCalledWith(32);
    expect(getAuditData().userAgentClientCustomRules).toEqual([createdRule]);
    expect(getAuditData().deleteUserAgentClientRuleConfirm).toBeNull();
    expect(getAuditData().displayedDeleteUserAgentClientRuleConfirm).toMatchObject({
      id: 32,
      name: "Codex Agent",
      pattern: "Codex-Agent",
    });
    expect(api.config.headerBlocklistRules.list).toHaveBeenCalledTimes(1);
  });

  it("surfaces backend regex validation errors instead of blocking submit in the frontend", async () => {
    api.config.userAgentClientRules.create.mockRejectedValue(new Error("Invalid Python regex"));

    const { result } = renderHook(() => useAuditConfigurationData({ revision: 3 }));

    await waitFor(() => {
      expect(result.current.loadingRules).toBe(false);
    });

    act(() => {
      const auditData = result.current as UserAgentClientAuditConfigurationSlice;
      auditData.openAddUserAgentClientRuleDialog();
      auditData.setUserAgentClientRuleForm({
        enabled: true,
        name: "Broken rule",
        pattern: "[Codex",
      });
    });

    await act(async () => {
      const auditData = result.current as UserAgentClientAuditConfigurationSlice;
      await auditData.handleSaveUserAgentClientRule();
    });

    expect(api.config.userAgentClientRules.create).toHaveBeenCalledWith({
      enabled: true,
      name: "Broken rule",
      pattern: "[Codex",
    });
    expect(toast.error).toHaveBeenCalledWith("Invalid Python regex");
  });
});
