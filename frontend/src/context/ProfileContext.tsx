import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, setApiProfileId } from "@/lib/api";
import { useLocale } from "@/i18n/useLocale";
import type { Profile, ProfileCreate, ProfileUpdate } from "@/lib/types";
import { createProfileBootstrapLoader } from "./profile/bootstrap";
import { createProfileActions } from "./profile/actions";
import { parseStoredProfileId, PROFILE_STORAGE_KEY, writeStoredProfileId } from "./profile/persistence";
import { resolveSelectedProfile } from "./profile/selection";

interface ProfileContextType {
  profiles: Profile[];
  activeProfile: Profile | null;
  maxProfiles: number;
  selectedProfile: Profile | null;
  selectedProfileId: number | null;
  isLoading: boolean;
  error: string | null;
  revision: number;
  selectProfile: (profileId: number) => void;
  refreshProfiles: () => Promise<Profile[]>;
  createProfile: (data: ProfileCreate) => Promise<Profile>;
  updateProfile: (id: number, data: ProfileUpdate) => Promise<Profile>;
  activateProfile: (id: number) => Promise<Profile>;
  deleteProfile: (id: number) => Promise<void>;
  bumpRevision: () => void;
}

const ProfileContext = createContext<ProfileContextType | undefined>(undefined);

const loadProfileBootstrapState = createProfileBootstrapLoader({
  bootstrap: () => api.profiles.bootstrap(),
});

export function ProfileProvider({ children }: { children: ReactNode }) {
  const { messages } = useLocale();
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeProfile, setActiveProfile] = useState<Profile | null>(null);
  const [maxProfiles, setMaxProfiles] = useState(0);
  const [selectedProfileId, setSelectedProfileId] = useState<number | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const profilesRef = useRef<Profile[]>([]);
  const activeProfileRef = useRef<Profile | null>(null);
  const selectedProfileIdRef = useRef<number | null>(null);

  const selectedProfile = useMemo(
    () => profiles.find((profile) => profile.id === selectedProfileId) ?? null,
    [profiles, selectedProfileId]
  );

  const bumpRevision = useCallback(() => {
    setRevision((previous) => previous + 1);
  }, []);

  const applyProfiles = useCallback((nextProfiles: Profile[]) => {
    profilesRef.current = nextProfiles;
    setProfiles(nextProfiles);
  }, []);

  const applyActiveProfile = useCallback((nextActiveProfile: Profile | null) => {
    activeProfileRef.current = nextActiveProfile;
    setActiveProfile(nextActiveProfile);
  }, []);

  const commitSelectedProfileId = useCallback(
    (profileId: number | null, options?: { bumpRevision?: boolean }) => {
      selectedProfileIdRef.current = profileId;
      setSelectedProfileId(profileId);
      setApiProfileId(profileId);
      writeStoredProfileId(profileId);
      if (options?.bumpRevision) {
        bumpRevision();
      }
    },
    [bumpRevision]
  );

  const syncSelectedProfile = useCallback(
    (
      snapshotProfiles: Profile[],
      activeProfileId: number | null,
      options?: {
        preferredProfileId?: number | null;
        bumpRevisionOnChange?: boolean;
      }
    ): number | null => {
      const candidate =
        options?.preferredProfileId === undefined
          ? selectedProfileIdRef.current
          : options.preferredProfileId;
      const nextSelected = resolveSelectedProfile(
        snapshotProfiles,
        candidate,
        activeProfileId
      );
      const nextSelectedId = nextSelected?.id ?? null;

      commitSelectedProfileId(nextSelectedId, {
        bumpRevision:
          Boolean(options?.bumpRevisionOnChange) &&
          nextSelectedId !== selectedProfileIdRef.current,
      });

      return nextSelectedId;
    },
    [commitSelectedProfileId]
  );

  const selectProfile = useCallback(
    (profileId: number) => {
      commitSelectedProfileId(profileId, { bumpRevision: true });
    },
    [commitSelectedProfileId]
  );

  const profileActions = useMemo(
    () =>
      createProfileActions({
        profilesApi: api.profiles,
        getActiveProfile: () => activeProfileRef.current,
        getProfiles: () => profilesRef.current,
        getSelectedProfileId: () => selectedProfileIdRef.current,
        setProfiles: applyProfiles,
        setActiveProfile: applyActiveProfile,
        syncSelectedProfile,
      }),
    [applyActiveProfile, applyProfiles, syncSelectedProfile]
  );

  useEffect(() => {
    let mounted = true;

    const bootstrap = async () => {
      setIsLoading(true);
      setError(null);

      try {
        const {
          profiles: fetchedProfiles,
          activeProfile: fetchedActiveProfile,
          maxProfiles: fetchedMaxProfiles,
        } =
          await loadProfileBootstrapState(true);
        if (!mounted) return;

        setMaxProfiles(fetchedMaxProfiles);
        applyProfiles(fetchedProfiles);
        applyActiveProfile(fetchedActiveProfile);

        const persistedId = parseStoredProfileId(
          localStorage.getItem(PROFILE_STORAGE_KEY)
        );
        syncSelectedProfile(
          fetchedProfiles,
          fetchedActiveProfile?.id ?? null,
          { preferredProfileId: persistedId }
        );
      } catch (err) {
        if (mounted) {
          const message = err instanceof Error ? err.message : messages.profiles.initializeFailed;
          setError(message);
        }
      } finally {
        if (mounted) {
          setIsLoading(false);
        }
      }
    };

    bootstrap();

    return () => {
      mounted = false;
    };
  }, [applyActiveProfile, applyProfiles, messages.profiles.initializeFailed, syncSelectedProfile]);

  if (isLoading) {
    return (
      <div className="flex h-screen w-full items-center justify-center bg-background text-muted-foreground">
        <div className="flex flex-col items-center gap-2">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
          <p className="text-sm font-medium">{messages.profiles.loadingProfiles}</p>
        </div>
      </div>
    );
  }

  return (
    <ProfileContext.Provider
      value={{
        profiles,
        activeProfile,
        maxProfiles,
        selectedProfile,
        selectedProfileId,
        isLoading,
        error,
        revision,
        selectProfile,
        refreshProfiles: profileActions.refreshProfiles,
        createProfile: profileActions.createProfile,
        updateProfile: profileActions.updateProfile,
        activateProfile: profileActions.activateProfile,
        deleteProfile: profileActions.deleteProfile,
        bumpRevision,
      }}
    >
      {children}
    </ProfileContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useProfileContext() {
  const context = useContext(ProfileContext);
  if (context === undefined) {
    throw new Error("useProfileContext must be used within a ProfileProvider");
  }
  return context;
}
