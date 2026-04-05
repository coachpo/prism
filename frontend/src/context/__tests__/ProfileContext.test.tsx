import { StrictMode } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { createProfileBootstrapLoader } from "../profile/bootstrap";
import { parseStoredProfileId } from "../profile/persistence";
import { resolveSelectedProfile } from "../profile/selection";
import { ProfileProvider, useProfileContext } from "../ProfileContext";

const api = vi.hoisted(() => ({
  profiles: {
    bootstrap: vi.fn(),
    list: vi.fn(),
  },
}));

const setApiProfileId = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", () => ({ api, setApiProfileId }));

const storageState = new Map<string, string>();
const localStorageMock = {
  clear: vi.fn(() => {
    storageState.clear();
  }),
  getItem: vi.fn((key: string) => storageState.get(key) ?? null),
  removeItem: vi.fn((key: string) => {
    storageState.delete(key);
  }),
  setItem: vi.fn((key: string, value: string) => {
    storageState.set(key, value);
  }),
};

Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: localStorageMock,
});

function ProfileProbe() {
  const { activeProfile, selectedProfile, selectProfile } = useProfileContext();

  return (
    <div>
      <div>{selectedProfile?.name ?? "missing"}/{activeProfile?.name ?? "missing"}</div>
      <button type="button" onClick={() => selectProfile(2)}>
        Select secondary
      </button>
    </div>
  );
}

describe("ProfileProvider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    storageState.clear();
    localStorage.clear();

    const profile = {
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
    };

    api.profiles.bootstrap.mockResolvedValue({
      profiles: [profile],
      active_profile: profile,
      profile_limits: { max_profiles: 10 },
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("deduplicates StrictMode bootstrap profile fetches", async () => {
    render(
      <LocaleProvider>
        <StrictMode>
          <ProfileProvider>
            <ProfileProbe />
          </ProfileProvider>
        </StrictMode>
      </LocaleProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("Default/Default")).toBeInTheDocument();
    });

    expect(api.profiles.bootstrap).toHaveBeenCalledTimes(1);
    expect(setApiProfileId).toHaveBeenCalledWith(1);
  });

  it("does not rerun bootstrap fetches when only the selected profile changes", async () => {
    const profiles = [
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
    ];

    api.profiles.bootstrap.mockResolvedValue({
      profiles,
      active_profile: profiles[0],
      profile_limits: { max_profiles: 10 },
    });

    render(
      <LocaleProvider>
        <StrictMode>
          <ProfileProvider>
            <ProfileProbe />
          </ProfileProvider>
        </StrictMode>
      </LocaleProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("Default/Default")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Select secondary" }));

    await waitFor(() => {
      expect(screen.getByText("Secondary/Default")).toBeInTheDocument();
    });

    expect(api.profiles.bootstrap).toHaveBeenCalledTimes(1);
  });

  it("fetches fresh profile snapshots after provider remounts", async () => {
    const firstProfile = {
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
    };
    const secondProfile = {
      id: 2,
      name: "Second login",
      description: null,
      is_active: true,
      is_default: true,
      is_editable: true,
      version: 1,
      created_at: "",
      deleted_at: null,
      updated_at: "",
    };

    api.profiles.bootstrap
      .mockResolvedValueOnce({
        profiles: [firstProfile],
        active_profile: firstProfile,
        profile_limits: { max_profiles: 10 },
      })
      .mockResolvedValueOnce({
        profiles: [secondProfile],
        active_profile: secondProfile,
        profile_limits: { max_profiles: 10 },
      });

    const firstRender = render(
      <LocaleProvider>
        <ProfileProvider>
          <ProfileProbe />
        </ProfileProvider>
      </LocaleProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("Default/Default")).toBeInTheDocument();
    });

    firstRender.unmount();

    render(
      <LocaleProvider>
        <ProfileProvider>
          <ProfileProbe />
        </ProfileProvider>
      </LocaleProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("Second login/Second login")).toBeInTheDocument();
    });

    expect(api.profiles.bootstrap).toHaveBeenCalledTimes(2);
  });

  it("reuses the in-flight bootstrap request when StrictMode bootstraps twice", async () => {
    const bootstrapDeferred = Promise.resolve({
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
      ],
      active_profile: {
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
      profile_limits: { max_profiles: 10 },
    });

    const bootstrap = vi.fn(() => bootstrapDeferred);
    const loadBootstrapState = createProfileBootstrapLoader({
      bootstrap,
    });

    const [first, second] = await Promise.all([
      loadBootstrapState(true),
      loadBootstrapState(true),
    ]);

    expect(first).toEqual(second);
    expect(bootstrap).toHaveBeenCalledTimes(1);
  });

  it("parses stored profile ids conservatively", () => {
    expect(parseStoredProfileId("12")).toBe(12);
    expect(parseStoredProfileId("0")).toBeNull();
    expect(parseStoredProfileId("abc")).toBeNull();
    expect(parseStoredProfileId(null)).toBeNull();
  });

  it("prefers a stored profile before default and active fallbacks", () => {
    const profiles = [
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
    ];

    expect(resolveSelectedProfile(profiles, 2, 1)?.id).toBe(2);
    expect(resolveSelectedProfile(profiles, 999, 1)?.id).toBe(1);
  });

  it("renders localized loading copy while profile bootstrap is pending", () => {
    localStorage.setItem("prism.locale", "zh-CN");
    api.profiles.bootstrap.mockImplementation(() => new Promise(() => {}));

    render(
      <LocaleProvider>
        <ProfileProvider>
          <div>content</div>
        </ProfileProvider>
      </LocaleProvider>,
    );

    expect(screen.getByText("正在加载配置档案...")).toBeInTheDocument();
  });

  it("refreshes profile snapshots through the extracted action helper", async () => {
    const { createProfileActions } = await import("../profile/actions");
    const refreshedProfiles = [
      {
        id: 1,
        name: "Default",
        description: null,
        is_active: false,
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
        is_active: true,
        is_default: false,
        is_editable: true,
        version: 1,
        created_at: "",
        deleted_at: null,
        updated_at: "",
      },
    ];
    const setProfiles = vi.fn();
    const setActiveProfile = vi.fn();
    const syncSelectedProfile = vi.fn();
    const actions = createProfileActions({
      profilesApi: {
        list: vi.fn().mockResolvedValue(refreshedProfiles),
        create: vi.fn(),
        update: vi.fn(),
        activate: vi.fn(),
        delete: vi.fn(),
      },
      getActiveProfile: () => refreshedProfiles[0],
      getProfiles: () => refreshedProfiles,
      getSelectedProfileId: () => 1,
      setProfiles,
      setActiveProfile,
      syncSelectedProfile,
    });

    await expect(actions.refreshProfiles()).resolves.toEqual(refreshedProfiles);

    expect(setProfiles).toHaveBeenCalledWith(refreshedProfiles);
    expect(setActiveProfile).toHaveBeenCalledWith(refreshedProfiles[1]);
    expect(syncSelectedProfile).toHaveBeenCalledWith(refreshedProfiles, 2, {
      bumpRevisionOnChange: true,
    });
  });

  it("reselects a remaining profile before refreshing after deleting the current selection", async () => {
    const { createProfileActions } = await import("../profile/actions");
    const profiles = [
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
    ];
    const syncSelectedProfile = vi.fn();
    const profilesApi = {
      list: vi.fn().mockResolvedValue([profiles[0]]),
      create: vi.fn(),
      update: vi.fn(),
      activate: vi.fn(),
      delete: vi.fn().mockResolvedValue(undefined),
    };
    const actions = createProfileActions({
      profilesApi,
      getActiveProfile: () => profiles[0],
      getProfiles: () => profiles,
      getSelectedProfileId: () => 2,
      setProfiles: vi.fn(),
      setActiveProfile: vi.fn(),
      syncSelectedProfile,
    });

    await actions.deleteProfile(2);

    expect(profilesApi.delete).toHaveBeenCalledWith(2);
    expect(syncSelectedProfile).toHaveBeenCalledWith([profiles[0]], 1, {
      bumpRevisionOnChange: true,
    });
    expect(profilesApi.list).toHaveBeenCalledTimes(1);
  });
});
