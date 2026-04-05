export const PROFILE_STORAGE_KEY = "prism.selectedProfileId";

export function parseStoredProfileId(raw: string | null): number | null {
  if (!raw) {
    return null;
  }

  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

export function writeStoredProfileId(profileId: number | null): void {
  if (profileId === null) {
    localStorage.removeItem(PROFILE_STORAGE_KEY);
    return;
  }

  localStorage.setItem(PROFILE_STORAGE_KEY, String(profileId));
}
