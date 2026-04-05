import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { toast } from "sonner";
import { useProfileDialogState } from "../useProfileDialogState";

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function LocaleWrapper({ children }: { children: ReactNode }) {
  return <LocaleProvider>{children}</LocaleProvider>;
}

describe("useProfileDialogState", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("emits a localized required-name validation toast", async () => {
    const { result } = renderHook(
      () =>
        useProfileDialogState({
          activateProfile: vi.fn(),
          canCreateProfile: true,
          closeProfileSwitcher: vi.fn(),
          createProfile: vi.fn(),
          deleteProfile: vi.fn(),
          hasMismatch: false,
          selectProfile: vi.fn(),
          selectedIsActive: false,
          selectedIsDefault: false,
          selectedProfile: null,
          updateProfile: vi.fn(),
        }),
      { wrapper: LocaleWrapper },
    );

    await act(async () => {
      await result.current.handleCreateProfile();
    });

    expect(toast.error).toHaveBeenCalledWith("配置档案名称为必填项");
  });
});
