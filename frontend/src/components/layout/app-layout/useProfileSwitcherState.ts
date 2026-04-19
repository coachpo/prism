import { useState } from "react";

interface UseProfileSwitcherStateInput {
  profiles: { id: number }[];
  selectedProfileId: number | null;
  selectProfile: (profileId: number) => void;
}

export function useProfileSwitcherState({
  profiles,
  selectedProfileId,
  selectProfile,
}: UseProfileSwitcherStateInput) {
  const [profileSwitcherOpen, setProfileSwitcherOpenState] = useState(false);

  const hasNoProfiles = profiles.length === 0;

  const setProfileSwitcherOpen = (open: boolean) => {
    setProfileSwitcherOpenState(open);
  };

  const closeProfileSwitcher = () => {
    setProfileSwitcherOpen(false);
  };

  const handleSelectProfile = (profileId: number) => {
    if (selectedProfileId === profileId) {
      closeProfileSwitcher();
      return;
    }

    selectProfile(profileId);
    closeProfileSwitcher();
  };

  return {
    closeProfileSwitcher,
    handleSelectProfile,
    hasNoProfiles,
    profileSwitcherOpen,
    setProfileSwitcherOpen,
  };
}
