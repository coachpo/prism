import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AppLayout } from "@/components/layout/AppLayout";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { useProfileContext } from "@/context/ProfileContext";
import { useAppLayoutState } from "../useAppLayoutState";

const SIDEBAR_STORAGE_KEY = "prism.sidebarCollapsed";

vi.mock("@/context/useAuth", () => ({
  useAuth: () => ({
    authEnabled: false,
    logout: vi.fn(),
    username: null,
  }),
}));

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: vi.fn(),
}));

vi.mock("../useProfileDialogState", () => ({
  useProfileDialogState: () => ({
    activateOpen: false,
    createOpen: false,
    deleteConfirmInput: "",
    deleteConfirmTarget: "",
    deleteError: null,
    deleteOpen: false,
    descriptionInput: "",
    editOpen: false,
    handleActivateProfile: vi.fn(),
    handleCreateProfile: vi.fn(),
    handleDeleteProfile: vi.fn(),
    handleEditProfile: vi.fn(),
    isActivating: false,
    isDeleteConfirmMatch: false,
    isDeleting: false,
    isSaving: false,
    nameInput: "",
    openActivateDialog: vi.fn(),
    openCreateDialog: vi.fn(),
    openDeleteDialog: vi.fn(),
    openEditDialog: vi.fn(),
    setActivateOpen: vi.fn(),
    setCreateOpen: vi.fn(),
    setDeleteConfirmInput: vi.fn(),
    setDeleteError: vi.fn(),
    setDeleteOpen: vi.fn(),
    setDescriptionInput: vi.fn(),
    setEditOpen: vi.fn(),
    setNameInput: vi.fn(),
  }),
}));

vi.mock("../useProfileSwitcherState", () => ({
  useProfileSwitcherState: () => ({
    closeProfileSwitcher: vi.fn(),
    filteredProfiles: [],
    handleSelectProfile: vi.fn(),
    hasNoMatches: false,
    hasNoProfiles: false,
    profileQuery: "",
    profileSearchInputRef: { current: null },
    profileSwitcherOpen: false,
    selectedProfileButtonRef: { current: null },
    setProfileQuery: vi.fn(),
    setProfileSwitcherOpen: vi.fn(),
  }),
}));

vi.mock("@/components/GlobalPreferencesControls", () => ({
  GlobalPreferencesControls: () => <div data-testid="global-preferences-controls" />,
}));

vi.mock("../ProfileSwitcherPopover", () => ({
  ProfileSwitcherPopover: () => <button type="button">profile-switcher</button>,
}));

vi.mock("../ProfileDialogs", () => ({
  ProfileDialogs: () => null,
}));

function ShellProbe() {
  const { canCreateProfile, desktopSidebarCollapsed, toggleDesktopSidebar } = useAppLayoutState();

  return (
    <>
      <span data-testid="can-create-profile">{String(canCreateProfile)}</span>
      <span data-testid="desktop-sidebar-collapsed">{String(desktopSidebarCollapsed)}</span>
      <button type="button" onClick={toggleDesktopSidebar}>
        toggle-sidebar
      </button>
    </>
  );
}

describe("AppLayout shell bootstrap contract", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.mocked(useProfileContext).mockReturnValue({
      activeProfile: {
        id: 1,
        name: "Default",
        description: null,
        is_active: true,
        is_default: true,
        is_editable: true,
        version: 1,
        created_at: "",
        deleted_at: null,
        updated_at: "",
      },
      bumpRevision: vi.fn(),
      createProfile: vi.fn(),
      deleteProfile: vi.fn(),
      error: null,
      isLoading: false,
      maxProfiles: 2,
      profiles: [
        {
          id: 1,
          name: "Default",
          description: null,
          is_active: true,
          is_default: true,
          is_editable: true,
          version: 1,
          created_at: "",
          deleted_at: null,
          updated_at: "",
        },
        {
          id: 2,
          name: "Secondary",
          description: null,
          is_active: false,
          is_default: false,
          is_editable: true,
          version: 1,
          created_at: "",
          deleted_at: null,
          updated_at: "",
        },
      ],
      refreshProfiles: vi.fn(),
      revision: 0,
      selectProfile: vi.fn(),
      selectedProfile: {
        id: 1,
        name: "Default",
        description: null,
        is_active: true,
        is_default: true,
        is_editable: true,
        version: 1,
        created_at: "",
        deleted_at: null,
        updated_at: "",
      },
      selectedProfileId: 1,
      updateProfile: vi.fn(),
      activateProfile: vi.fn(),
    } as unknown as ReturnType<typeof useProfileContext>);
  });

  it("uses bootstrap max_profiles when computing whether the shell can create more profiles", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <ShellProbe />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByTestId("can-create-profile")).toHaveTextContent("false");
  });

  it("reads persisted desktop collapse state and falls back to expanded for invalid values", () => {
    localStorage.setItem(SIDEBAR_STORAGE_KEY, "true");

    const view = render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <ShellProbe />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByTestId("desktop-sidebar-collapsed")).toHaveTextContent("true");

    view.unmount();
    localStorage.setItem(SIDEBAR_STORAGE_KEY, "not-a-boolean");

    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <ShellProbe />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByTestId("desktop-sidebar-collapsed")).toHaveTextContent("false");
  });

  it("writes desktop collapse state to localStorage when toggled", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/dashboard"]}>
          <ShellProbe />
        </MemoryRouter>
      </LocaleProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "toggle-sidebar" }));

    expect(localStorage.getItem(SIDEBAR_STORAGE_KEY)).toBe("true");
    expect(screen.getByTestId("desktop-sidebar-collapsed")).toHaveTextContent("true");
  });

  it("renders the provider-based shell chrome around protected routes", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/request-logs?request_id=42"]}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/request-logs" element={<div data-testid="protected-route-child">request-logs-child</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByTestId("shell-sidebar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse sidebar" })).toHaveAttribute("title", "Collapse sidebar");
    expect(screen.queryByTestId("shell-sidebar-rail")).not.toBeInTheDocument();
    expect(screen.queryByTestId("shell-sidebar-trigger")).not.toBeInTheDocument();
    expect(screen.getByTestId("shell-breadcrumb")).toBeInTheDocument();
    expect(screen.getByTestId("shell-breadcrumb-current")).toHaveTextContent("#42");
    expect(screen.getByTestId("shell-profile-switcher")).toBeInTheDocument();
    expect(screen.getByTestId("protected-route-child")).toBeInTheDocument();
  });
});
