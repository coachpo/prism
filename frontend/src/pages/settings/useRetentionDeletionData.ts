import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useManualCleanup } from "./useManualCleanup";
import { useRetentionJobDetails } from "./useRetentionJobDetails";
import { useRetentionJobList } from "./useRetentionJobList";
import { useRetentionPolicy } from "./useRetentionPolicy";

interface UseRetentionDeletionDataInput {
  enabled: boolean;
  setRecentlySavedSection?: (section: SettingsSaveSection | null) => void;
}

/**
 * Settings page coordinator: policy, manual cleanup, and durable job state each
 * own their lifecycle; this boundary preserves the page's combined data shape.
 */
export function useRetentionDeletionData({
  enabled,
  setRecentlySavedSection,
}: UseRetentionDeletionDataInput) {
  const { calibrateJobs, ...pageJobs } = useRetentionJobList({ enabled });
  const jobDetails = useRetentionJobDetails();
  const policy = useRetentionPolicy({
    enabled,
    onJobsMutation: calibrateJobs,
    setRecentlySavedSection,
  });
  const { refreshRetentionSettings, ...pagePolicy } = policy;
  const manual = useManualCleanup({
    onJobsMutation: calibrateJobs,
    refreshRetentionSettings,
  });

  return {
    ...pagePolicy,
    ...manual,
    ...pageJobs,
    ...jobDetails,
  };
}
