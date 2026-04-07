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
  });

  it("blocks create when the profile name is blank", async () => {
    const createProfile = vi.fn();

    const { result } = renderHook(
      () =>
        useProfileDialogState({
          activateProfile: vi.fn(),
          canCreateProfile: true,
          closeProfileSwitcher: vi.fn(),
          createProfile,
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

    expect(createProfile).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledTimes(1);
  });
});
