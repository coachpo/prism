import { createElement } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Profile } from "@/lib/types";
import { ProfileProvider, useProfileContext } from "@/context/ProfileContext";
import { createProfileBootstrapLoader } from "../bootstrap";
import { PROFILE_STORAGE_KEY } from "../persistence";

const api = vi.hoisted(() => ({
  profiles: {
    bootstrap: vi.fn(),
  },
}));

const setApiProfileId = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", () => ({ api, setApiProfileId }));

function makeProfile(overrides: Partial<Profile> = {}): Profile {
  return {
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
    ...overrides,
  };
}

function ProfileProbe() {
  const { activeProfile, selectedProfile, error, maxProfiles } = useProfileContext();

  return createElement(
    "div",
    null,
    createElement("span", { "data-testid": "selected-profile" }, selectedProfile?.name ?? "none"),
    createElement("span", { "data-testid": "active-profile" }, activeProfile?.name ?? "none"),
    createElement("span", { "data-testid": "profile-error" }, error ?? "none"),
    createElement("span", { "data-testid": "max-profiles" }, String(maxProfiles)),
  );
}

describe("profile bootstrap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("reuses one bootstrap request and maps the shell payload", async () => {
    const defaultProfile = makeProfile();
    const bootstrap = vi.fn().mockResolvedValue({
      profiles: [defaultProfile],
      active_profile: defaultProfile,
      profile_limits: { max_profiles: 7 },
    });

    const loadBootstrapState = createProfileBootstrapLoader({ bootstrap });
    const [first, second] = await Promise.all([
      loadBootstrapState(true),
      loadBootstrapState(true),
    ]);

    expect(first).toEqual({
      activeProfile: defaultProfile,
      profiles: [defaultProfile],
      maxProfiles: 7,
    });
    expect(second).toEqual(first);
    expect(bootstrap).toHaveBeenCalledTimes(1);
  });

  it("keeps the stored selection when bootstrap has no active profile", async () => {
    const defaultProfile = makeProfile({
      id: 1,
      is_active: false,
      name: "Default",
    });
    const secondaryProfile = makeProfile({
      id: 2,
      is_active: false,
      is_default: false,
      name: "Secondary",
    });
    localStorage.setItem(PROFILE_STORAGE_KEY, "2");
    api.profiles.bootstrap.mockResolvedValue({
      profiles: [defaultProfile, secondaryProfile],
      active_profile: null,
      profile_limits: { max_profiles: 2 },
    });

    render(
      createElement(
        LocaleProvider,
        null,
        createElement(ProfileProvider, null, createElement(ProfileProbe)),
      ),
    );

    await waitFor(() => {
      expect(screen.getByTestId("selected-profile")).toHaveTextContent("Secondary");
    });

    expect(screen.getByTestId("active-profile")).toHaveTextContent("none");
    expect(screen.getByTestId("profile-error")).toHaveTextContent("none");
    expect(screen.getByTestId("max-profiles")).toHaveTextContent("2");
    expect(setApiProfileId).toHaveBeenLastCalledWith(2);
  });

  it("exposes the localized bootstrap failure without committing stale profile state", async () => {
    api.profiles.bootstrap.mockRejectedValue({ detail: "boom" });

    render(
      createElement(
        LocaleProvider,
        null,
        createElement(ProfileProvider, null, createElement(ProfileProbe)),
      ),
    );

    await waitFor(() => {
      expect(screen.getByTestId("profile-error")).toHaveTextContent(
        "Failed to initialize profiles",
      );
    });

    expect(screen.getByTestId("selected-profile")).toHaveTextContent("none");
    expect(screen.getByTestId("active-profile")).toHaveTextContent("none");
    expect(setApiProfileId).not.toHaveBeenCalled();
  });
});
