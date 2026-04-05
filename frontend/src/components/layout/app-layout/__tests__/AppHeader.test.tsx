import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { AppHeader } from "../AppHeader";

vi.mock("next-themes", () => ({
  useTheme: () => ({
    resolvedTheme: "light",
    setTheme: vi.fn(),
    theme: "system",
  }),
}));

describe("AppHeader", () => {
  it("renders locale-backed mismatch and action copy", async () => {
    render(
      <LocaleProvider>
        <AppHeader
          activeProfileName="Runtime"
          authEnabled={true}
          canCreateProfile={true}
          deleteDisabledReason={null}
          editDisabledReason={null}
          filteredProfiles={[]}
          handleLogout={vi.fn()}
          handleManageProfiles={vi.fn()}
          handleSelectProfile={vi.fn()}
          hasMismatch={true}
          hasNoMatches={false}
          hasNoProfiles={false}
          isActivating={false}
          isProfileScopedPage={true}
          openActivateDialog={vi.fn()}
          openCreateDialog={vi.fn()}
          openDeleteDialog={vi.fn()}
          openEditDialog={vi.fn()}
          profileQuery=""
          profileSearchInputRef={{ current: null }}
          profileSwitcherOpen={false}
          selectedIsActive={false}
          selectedProfileButtonRef={{ current: null }}
          selectedProfileId={1}
          selectedProfileName="Viewing"
          setProfileQuery={vi.fn()}
          setProfileSwitcherOpen={vi.fn()}
          username="alice"
        />
      </LocaleProvider>,
    );

    expect(screen.getByRole("button", { name: /^English/ })).toBeInTheDocument();
    expect(screen.queryByText("简体中文")).not.toBeInTheDocument();
    expect(screen.getByText(/activate profile/i)).toBeInTheDocument();
    expect(screen.getByText(/you're viewing/i)).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();

    const languageTrigger = screen.getByRole("button", { name: /^English/ });
    languageTrigger.focus();
    fireEvent.keyDown(languageTrigger, { key: "Enter", code: "Enter" });

    await waitFor(() => {
      expect(screen.getByRole("menuitem", { name: "简体中文" })).toBeInTheDocument();
    });
  });

  it("stacks the profile switcher and global preferences controls for mobile widths", () => {
    render(
      <LocaleProvider>
        <AppHeader
          activeProfileName="Runtime"
          authEnabled={false}
          canCreateProfile={true}
          deleteDisabledReason={null}
          editDisabledReason={null}
          filteredProfiles={[]}
          handleLogout={vi.fn()}
          handleManageProfiles={vi.fn()}
          handleSelectProfile={vi.fn()}
          hasMismatch={false}
          hasNoMatches={false}
          hasNoProfiles={false}
          isActivating={false}
          isProfileScopedPage={true}
          openActivateDialog={vi.fn()}
          openCreateDialog={vi.fn()}
          openDeleteDialog={vi.fn()}
          openEditDialog={vi.fn()}
          profileQuery=""
          profileSearchInputRef={{ current: null }}
          profileSwitcherOpen={false}
          selectedIsActive={true}
          selectedProfileButtonRef={{ current: null }}
          selectedProfileId={1}
          selectedProfileName="Viewing"
          setProfileQuery={vi.fn()}
          setProfileSwitcherOpen={vi.fn()}
          username={null}
        />
      </LocaleProvider>,
    );

    const profileTrigger = screen.getByRole("combobox");
    const controlsGroup = profileTrigger.parentElement;

    expect(profileTrigger).toHaveClass("w-full");
    expect(profileTrigger).toHaveClass("sm:w-[320px]");
    expect(controlsGroup).not.toBeNull();
    expect(controlsGroup).toHaveClass("w-full");
    expect(controlsGroup).toHaveClass("flex-col");
    expect(controlsGroup).toHaveClass("items-stretch");
    expect(controlsGroup).toHaveClass("sm:flex-row");
    expect(controlsGroup).toHaveClass("sm:items-center");
    expect(controlsGroup).toHaveClass("sm:justify-end");
  });
});
