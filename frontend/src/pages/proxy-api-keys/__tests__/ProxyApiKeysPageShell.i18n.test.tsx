import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ProxyApiKeysPage } from "@/pages/ProxyApiKeysPage";

vi.mock("../useProxyApiKeysPageData", () => ({
  useProxyApiKeysPageData: () => ({
    pageLoading: false,
    authStatusTone: "tone",
    authStatusLabel: "身份验证关闭",
    authSettings: { auth_enabled: false },
    createDisabled: false,
    creatingProxyKey: false,
    handleCreateSubmit: vi.fn(),
    latestGeneratedKey: null,
    proxyKeyLimit: 100,
    proxyKeyName: "",
    proxyKeyNotes: "",
    proxyKeys: [],
    displayedProxyKeys: [],
    remainingKeys: 100,
    setProxyKeyName: vi.fn(),
    setProxyKeyNotes: vi.fn(),
    rotatingProxyKeyId: null,
    deletingProxyKeyId: null,
    handleRotateProxyKey: vi.fn(),
    startEditingProxyKey: vi.fn(),
    setDeleteConfirm: vi.fn(),
    editingProxyKey: null,
    editingProxyKeyActive: true,
    editingProxyKeyName: "",
    editingProxyKeyNotes: "",
    savingEditedProxyKeyId: null,
    handleEditDialogOpenChange: vi.fn(),
    handleEditSubmit: vi.fn(),
    setEditingProxyKeyActive: vi.fn(),
    setEditingProxyKeyName: vi.fn(),
    setEditingProxyKeyNotes: vi.fn(),
    deleteConfirm: null,
    handleDeleteProxyKey: vi.fn(),
    handleDeleteDialogOpenChange: vi.fn(),
  }),
}));

describe("ProxyApiKeysPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized page header copy", () => {
    render(
      <LocaleProvider>
        <ProxyApiKeysPage />
      </LocaleProvider>,
    );

    expect(screen.getByText("代理 API 密钥")).toBeInTheDocument();
    expect(screen.getByText("管理上游客户端用于访问 Prism 代理的机器凭证。适用于所有配置档案。")) .toBeInTheDocument();
  });
});
