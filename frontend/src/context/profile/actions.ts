import type { Profile, ProfileCreate, ProfileUpdate } from "@/lib/types";

export interface ProfileActionsApi {
  list: () => Promise<Profile[]>;
  create: (data: ProfileCreate) => Promise<Profile>;
  update: (id: number, data: ProfileUpdate) => Promise<Profile>;
  activate: (
    id: number,
    payload: { expected_active_profile_id: number },
  ) => Promise<Profile>;
  delete: (id: number) => Promise<void>;
}

interface SyncSelectedProfileOptions {
  preferredProfileId?: number | null;
  bumpRevisionOnChange?: boolean;
}

export interface ProfileActionsOptions {
  profilesApi: ProfileActionsApi;
  getActiveProfile: () => Profile | null;
  getProfiles: () => Profile[];
  getSelectedProfileId: () => number | null;
  setProfiles: (profiles: Profile[]) => void;
  setActiveProfile: (profile: Profile | null) => void;
  syncSelectedProfile: (
    profiles: Profile[],
    activeProfileId: number | null,
    options?: SyncSelectedProfileOptions,
  ) => number | null;
}

function applyProfileSnapshot(
  profiles: Profile[],
  { setProfiles, setActiveProfile, syncSelectedProfile }: Pick<
    ProfileActionsOptions,
    "setProfiles" | "setActiveProfile" | "syncSelectedProfile"
  >,
): Profile[] {
  setProfiles(profiles);

  const nextActiveProfile = profiles.find((profile) => profile.is_active) ?? null;
  setActiveProfile(nextActiveProfile);
  syncSelectedProfile(profiles, nextActiveProfile?.id ?? null, {
    bumpRevisionOnChange: true,
  });

  return profiles;
}

import { getStaticMessages } from "@/i18n/staticMessages";

export function createProfileActions(options: ProfileActionsOptions) {
  const refreshProfiles = async () => {
    const profiles = await options.profilesApi.list();
    return applyProfileSnapshot(profiles, options);
  };

  const createProfile = async (data: ProfileCreate) => {
    try {
      const createdProfile = await options.profilesApi.create(data);
      await refreshProfiles();
      return createdProfile;
    } catch (error) {
      if (error instanceof Error && error.message.includes("409")) {
        throw new Error(getStaticMessages().profiles.limitReached);
      }

      throw error;
    }
  };

  const updateProfile = async (id: number, data: ProfileUpdate) => {
    const updatedProfile = await options.profilesApi.update(id, data);
    await refreshProfiles();
    return updatedProfile;
  };

  const activateProfile = async (id: number) => {
    const activeProfile = options.getActiveProfile();
    if (!activeProfile) {
      throw new Error("No active profile snapshot available");
    }

    try {
      const updatedProfile = await options.profilesApi.activate(id, {
        expected_active_profile_id: activeProfile.id,
      });
      await refreshProfiles();
      return updatedProfile;
    } catch (error) {
      if (error instanceof Error && error.message.includes("409")) {
        await refreshProfiles();
      }

      throw error;
    }
  };

  const deleteProfile = async (id: number) => {
    await options.profilesApi.delete(id);

    if (options.getSelectedProfileId() === id) {
      const profiles = options.getProfiles();
      const activeProfile = options.getActiveProfile();
      const remainingProfiles = profiles.filter((profile) => profile.id !== id);
      const remainingActiveProfileId =
        activeProfile?.id === id ? null : (activeProfile?.id ?? null);

      options.syncSelectedProfile(remainingProfiles, remainingActiveProfileId, {
        bumpRevisionOnChange: true,
      });
    }

    await refreshProfiles();
  };

  return {
    refreshProfiles,
    createProfile,
    updateProfile,
    activateProfile,
    deleteProfile,
  };
}
