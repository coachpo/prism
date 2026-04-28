import type { Profile } from "@/lib/types";

export function resolveSelectedProfile(
  profiles: Profile[],
  storedProfileId: number | null,
  activeProfileId: number | null,
): Profile | null {
  if (storedProfileId !== null) {
    const storedProfile = profiles.find((profile) => profile.id === storedProfileId);
    if (storedProfile) {
      return storedProfile;
    }
  }

  if (activeProfileId !== null) {
    const activeProfile = profiles.find((profile) => profile.id === activeProfileId);
    if (activeProfile) {
      return activeProfile;
    }
  }

  const defaultProfile = profiles.find((profile) => profile.is_default);
  if (defaultProfile) {
    return defaultProfile;
  }

  return profiles[0] ?? null;
}
