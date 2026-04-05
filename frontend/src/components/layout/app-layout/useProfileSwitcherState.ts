import { useEffect, useMemo, useRef, useState } from "react";
import type { Profile } from "@/lib/types";

interface UseProfileSwitcherStateInput {
  profiles: Profile[];
  selectedProfileId: number | null;
  selectProfile: (profileId: number) => void;
}

export function useProfileSwitcherState({
  profiles,
  selectedProfileId,
  selectProfile,
}: UseProfileSwitcherStateInput) {
  const [profileSwitcherOpen, setProfileSwitcherOpenState] = useState(false);
  const [profileQuery, setProfileQuery] = useState("");
  const selectedProfileButtonRef = useRef<HTMLButtonElement | null>(null);
  const profileSearchInputRef = useRef<HTMLInputElement | null>(null);

  const filteredProfiles = useMemo(() => {
    const query = profileQuery.trim().toLowerCase();
    if (!query) return profiles;
    return profiles.filter((profile) => {
      const nameMatch = profile.name.toLowerCase().includes(query);
      const descriptionMatch = (profile.description ?? "").toLowerCase().includes(query);
      return nameMatch || descriptionMatch;
    });
  }, [profileQuery, profiles]);

  const hasProfiles = profiles.length > 0;
  const hasNoProfiles = !hasProfiles;
  const hasNoMatches = hasProfiles && filteredProfiles.length === 0;

  const setProfileSwitcherOpen = (open: boolean) => {
    if (!open) {
      setProfileQuery("");
    }
    setProfileSwitcherOpenState(open);
  };

  useEffect(() => {
    if (!profileSwitcherOpen) return;

    const frameId = window.requestAnimationFrame(() => {
      const selectedButton = selectedProfileButtonRef.current;
      if (selectedButton) {
        selectedButton.scrollIntoView({ block: "nearest" });
        selectedButton.focus({ preventScroll: true });
        return;
      }

      profileSearchInputRef.current?.focus();
    });

    return () => {
      window.cancelAnimationFrame(frameId);
    };
  }, [profileSwitcherOpen, selectedProfileId]);

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
    filteredProfiles,
    handleSelectProfile,
    hasNoMatches,
    hasNoProfiles,
    profileQuery,
    profileSearchInputRef,
    profileSwitcherOpen,
    selectedProfileButtonRef,
    setProfileQuery,
    setProfileSwitcherOpen,
  };
}
