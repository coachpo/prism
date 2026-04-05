import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { SettingsPage } from "@/pages/SettingsPage";
import { SettingsGlobalTab } from "../SettingsGlobalTab";
import type { SettingsPageData } from "../useSettingsPageData";

vi.mock("../useSettingsPageData", () => ({
  useSettingsPageData: () => ({
    selectedProfileLabel: "Default (#1)",
    renderSaveStateForSection: () => null,
    exporting: false,
    handleExport: vi.fn(),
    fileInputRef: { current: null },
    handleFileSelect: vi.fn(),
    selectedFile: null,
    parsedConfig: null,
    previewResult: null,
    importSummary: null,
    importing: false,
    handleImport: vi.fn(),
    billingDirty: false,
    handleSaveCostingSettings: vi.fn(),
    costingUnavailable: false,
    costingLoading: false,
    costingSaving: false,
    costingForm: {},
    setCostingForm: vi.fn(),
    normalizedCurrentCosting: {},
    nativeModels: [],
    modelLabelMap: new Map(),
    mappingConnections: [],
    mappingLoading: false,
    mappingModelId: "",
    setMappingModelId: vi.fn(),
    loadMappingConnections: vi.fn(),
    mappingEndpointId: "",
    setMappingEndpointId: vi.fn(),
    mappingEndpointOptions: [],
    mappingFxRate: "",
    setMappingFxRate: vi.fn(),
    addMappingFxError: null,
    handleAddFxMapping: vi.fn(),
    editingMappingKey: null,
    editingMappingFxRate: "",
    setEditingMappingFxRate: vi.fn(),
    editMappingFxError: null,
    handleSaveEditFxMapping: vi.fn(),
    handleCancelEditFxMapping: vi.fn(),
    handleStartEditFxMapping: vi.fn(),
    handleDeleteFxMapping: vi.fn(),
    timezoneDirty: false,
    timezonePreviewText: "preview",
    timezonePreviewZone: "UTC",
    vendors: [],
    toggleAudit: vi.fn(),
    toggleBodies: vi.fn(),
    loadingRules: false,
    systemRulesOpen: true,
    setSystemRulesOpen: vi.fn(),
    systemRules: [],
    userRulesOpen: true,
    setUserRulesOpen: vi.fn(),
    customRules: [],
    handleToggleRule: vi.fn(),
    openAddRuleDialog: vi.fn(),
    openEditRuleDialog: vi.fn(),
    setDeleteRuleConfirm: vi.fn(),
    cleanupType: "",
    setCleanupType: vi.fn(),
    retentionPreset: "",
    setRetentionPreset: vi.fn(),
    deleting: false,
    handleOpenDeleteConfirm: vi.fn(),
    authSettings: null,
    authEnabledInput: false,
    authUsername: "admin",
    setAuthUsername: vi.fn(),
    authEmail: "",
    setAuthEmail: vi.fn(),
    authPassword: "",
    authPasswordError: null,
    setAuthPassword: vi.fn(),
    authPasswordConfirm: "",
    authPasswordMismatch: false,
    setAuthPasswordConfirm: vi.fn(),
    emailVerificationOtp: "",
    setEmailVerificationOtp: vi.fn(),
    sendingEmailVerification: false,
    confirmingEmailVerification: false,
    handleRequestEmailVerification: vi.fn(),
    handleConfirmEmailVerification: vi.fn(),
    authSaving: false,
    handleSaveAuthSettings: vi.fn(),
    deleteConfirm: null,
    setDeleteConfirm: vi.fn(),
    deleteConfirmPhrase: "",
    setDeleteConfirmPhrase: vi.fn(),
    handleBatchDelete: vi.fn(),
    isDeletePhraseValid: false,
    ruleDialogOpen: false,
    setRuleDialogOpen: vi.fn(),
    editingRule: null,
    ruleForm: { enabled: true, match_type: "exact", name: "", pattern: "" },
    setRuleForm: vi.fn(),
    handleSaveRule: vi.fn(),
    deleteRuleConfirm: null,
    handleDeleteRule: vi.fn(),
  }),
}));

vi.mock("../useSettingsPageSectionState", () => ({
  useSettingsPageSectionState: () => ({
    activeTab: "profile",
    setActiveTab: vi.fn(),
    activeSectionId: "audit-configuration",
    setActiveSectionId: vi.fn(),
    isAuditConfigurationFocused: true,
    jumpToSection: vi.fn(),
  }),
}));

vi.mock("../sections/BackupSection", () => ({ BackupSection: () => <div>backup section</div> }));
vi.mock("../sections/BillingCurrencySection", () => ({ BillingCurrencySection: () => <div>billing section</div> }));
vi.mock("../sections/TimezoneSection", () => ({ TimezoneSection: () => <div>timezone section</div> }));
vi.mock("../sections/AuditConfigurationSection", () => ({ AuditConfigurationSection: () => <div>audit section</div> }));
vi.mock("../sections/AuthenticationSection", () => ({ AuthenticationSection: () => <div>auth section</div> }));
vi.mock("../sections/VendorManagementSection", () => ({ VendorManagementSection: () => <div>vendor section</div> }));
vi.mock("../sections/RetentionDeletionSection", () => ({ RetentionDeletionSection: () => <div>retention section</div> }));
vi.mock("../dialogs/DeleteConfirmDialog", () => ({ DeleteConfirmDialog: () => null }));
vi.mock("../dialogs/RuleDialog", () => ({ RuleDialog: () => null }));
vi.mock("../dialogs/DeleteRuleConfirmDialog", () => ({ DeleteRuleConfirmDialog: () => null }));
vi.mock("../dialogs/VendorDialog", () => ({ VendorDialog: () => null }));
vi.mock("../dialogs/DeleteVendorDialog", () => ({ DeleteVendorDialog: () => null }));

describe("SettingsPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized shell copy and section nav labels", () => {
    render(
      <LocaleProvider>
        <MemoryRouter>
          <SettingsPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByText("设置")).toBeInTheDocument();
    expect(screen.getByText("管理实例范围的身份验证和按配置档案划分的配置")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "配置档案" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "全局" })).toBeInTheDocument();
    expect(screen.getByText("配置档案作用域设置")).toBeInTheDocument();
    expect(screen.getByText("设置分区")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "审计与隐私" })).toBeInTheDocument();
  });

  it("renders authentication before vendor management in the global tab", () => {
    const data = {
      authSettings: null,
      authEnabledInput: false,
      authUsername: "admin",
      setAuthUsername: vi.fn(),
      authEmail: "",
      setAuthEmail: vi.fn(),
      authPassword: "",
      authPasswordError: null,
      setAuthPassword: vi.fn(),
      authPasswordConfirm: "",
      authPasswordMismatch: false,
      setAuthPasswordConfirm: vi.fn(),
      emailVerificationOtp: "",
      setEmailVerificationOtp: vi.fn(),
      sendingEmailVerification: false,
      confirmingEmailVerification: false,
      handleRequestEmailVerification: vi.fn(),
      handleConfirmEmailVerification: vi.fn(),
      authSaving: false,
      handleSaveAuthSettings: vi.fn(),
      vendors: [],
      vendorsLoading: false,
      openCreateVendorDialog: vi.fn(),
      handleEditVendor: vi.fn(),
      handleDeleteVendorClick: vi.fn(),
      vendorDialogOpen: false,
      closeVendorDialog: vi.fn(),
      editingVendor: null,
      vendorForm: { key: "", name: "", description: "" },
      setVendorForm: vi.fn(),
      handleSaveVendor: vi.fn(),
      vendorSaving: false,
      deleteVendorConfirm: null,
      closeDeleteVendorDialog: vi.fn(),
      setDeleteVendorConfirm: vi.fn(),
      handleDeleteVendor: vi.fn(),
      vendorDeleting: false,
      vendorUsageLoading: false,
      vendorUsageRows: [],
      deleteVendorConflict: null,
    } as unknown as SettingsPageData;

    render(
      <LocaleProvider>
        <SettingsGlobalTab data={data} />
      </LocaleProvider>,
    );

    expect(screen.getByText("auth section")).toBeInTheDocument();
    expect(screen.getByText("vendor section")).toBeInTheDocument();
  });
});
