import type { Profile, ProfileBootstrapResponse } from "@/lib/types";

export interface ProfileBootstrapState {
  activeProfile: Profile | null;
  maxProfiles: number;
  profiles: Profile[];
}

export interface ProfileBootstrapApi {
  bootstrap: () => Promise<ProfileBootstrapResponse>;
}

export function createProfileBootstrapLoader(api: ProfileBootstrapApi) {
  let profileBootstrapPromise: Promise<ProfileBootstrapState> | null = null;

  return async function loadProfileBootstrapState(
    reuseInFlight = false,
  ): Promise<ProfileBootstrapState> {
    if (reuseInFlight && profileBootstrapPromise) {
      return profileBootstrapPromise;
    }

    const loadPromise = api.bootstrap().then(
      ({ active_profile: activeProfile, profile_limits, profiles }) => ({
        activeProfile,
        maxProfiles: profile_limits.max_profiles,
        profiles,
      }),
    );

    if (reuseInFlight) {
      profileBootstrapPromise = loadPromise;
      void loadPromise
        .finally(() => {
          if (profileBootstrapPromise === loadPromise) {
            profileBootstrapPromise = null;
          }
        })
        .catch(() => undefined);
    }

    return loadPromise;
  };
}
