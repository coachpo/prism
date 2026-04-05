import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PasskeysCard } from "../PasskeysCard";

vi.mock("../usePasskeyManagement", () => ({
  usePasskeyManagement: () => ({
    deviceName: "",
    handleRegisterClick: vi.fn(),
    handleRegisterSubmit: vi.fn(),
    handleRemoveClick: vi.fn(),
    handleRemoveConfirm: vi.fn(),
    isPasskeySupported: true,
    loadingPasskeys: false,
    passkeyToRemove: null,
    passkeys: [],
    registerDialogOpen: false,
    registering: false,
    removeDialogOpen: false,
    removing: false,
    setDeviceName: vi.fn(),
    setRegisterDialogOpen: vi.fn(),
    setRemoveDialogOpen: vi.fn(),
  }),
}));

describe("PasskeysCard i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized passkey empty-state copy when authentication is disabled", () => {
    render(
      <LocaleProvider>
        <PasskeysCard authEnabled={false} />
      </LocaleProvider>,
    );

    expect(screen.getByText("通行密钥")).toBeInTheDocument();
    expect(screen.getByText("身份验证已禁用")).toBeInTheDocument();
    expect(screen.getByText("启用身份验证后即可注册和管理通行密钥。")) .toBeInTheDocument();
  });
});
