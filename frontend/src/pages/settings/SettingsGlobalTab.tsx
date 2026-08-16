import { SettingsSectionsNav } from "./SettingsSectionsNav";
import type { SettingsPageData } from "./useSettingsPageData";
import { AuthenticationSection } from "./sections/AuthenticationSection";
import { ManualCleanupSection } from "./sections/ManualCleanupSection";
import { RetentionDeletionSection } from "./sections/RetentionDeletionSection";
import { RetentionJobsSection } from "./sections/RetentionJobsSection";

interface SettingsGlobalTabProps {
  activeSectionId: string | null;
  data: SettingsPageData;
  onJumpToSection: (sectionId: string) => void;
  onRetentionInvalidChange: (hasInvalid: boolean) => void;
}

export function SettingsGlobalTab({
  activeSectionId,
  data,
  onJumpToSection,
  onRetentionInvalidChange,
}: SettingsGlobalTabProps) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-4 lg:grid lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-6">
        <aside className="lg:sticky lg:top-4 lg:h-fit">
          <SettingsSectionsNav
            activeSectionId={activeSectionId ?? ""}
            onJumpToSection={onJumpToSection}
            scope="instance"
          />
        </aside>
        <div className="flex flex-col gap-5">
          <AuthenticationSection
            authSettings={data.authSettings}
            authEnabled={data.authEnabledInput}
            username={data.authUsername}
            setUsername={data.setAuthUsername}
            password={data.authPassword}
            passwordError={data.authPasswordError}
            setPassword={data.setAuthPassword}
            passwordConfirm={data.authPasswordConfirm}
            passwordMismatch={data.authPasswordMismatch}
            setPasswordConfirm={data.setAuthPasswordConfirm}
            authSaving={data.authSaving}
            onSaveAuthSettings={data.handleSaveAuthSettings}
            pendingAuthConfirmation={data.pendingAuthConfirmation}
            onConfirmAuthSettings={data.confirmPendingAuthSettings}
            onCancelAuthSettingsConfirmation={data.cancelPendingAuthConfirmation}
          />

          <RetentionDeletionSection
            renderSectionSaveState={data.renderSaveStateForSection}
            retentionSettings={data.retentionSettings}
            retentionSettingsDirty={data.retentionSettingsDirty}
            retentionSettingsLoading={data.retentionSettingsLoading}
            setRetentionDays={data.setRetentionDays}
            applyRecommendation={data.applyRecommendation}
            onInvalidCustomValueChange={onRetentionInvalidChange}
          />

          <ManualCleanupSection
            cleanupType={data.cleanupType}
            deleting={data.deleting}
            handleOpenDeleteConfirm={data.handleOpenDeleteConfirm}
            preflightLoading={data.manualPreflightLoading}
            retentionPreset={data.retentionPreset}
            setCleanupType={data.setCleanupType}
            setRetentionPreset={data.setRetentionPreset}
          />

          <RetentionJobsSection
            jobs={data.jobs}
            jobsHasMore={data.jobsHasMore}
            jobsLoading={data.jobsLoading}
            jobsStale={data.jobsStale}
            jobOriginFilter={data.jobOriginFilter}
            jobStateFilter={data.jobStateFilter}
            setJobOriginFilter={data.setJobOriginFilter}
            setJobStateFilter={data.setJobStateFilter}
            loadMoreJobs={data.loadMoreJobs}
            handleCancelJob={data.handleCancelJob}
            openJobDetail={data.openJobDetail}
            selectedJob={data.selectedJob}
            jobDetail={data.jobDetail}
            jobDetailLoading={data.jobDetailLoading}
            setSelectedJob={data.setSelectedJob}
            loadMoreJobCheckpoints={data.loadMoreJobCheckpoints}
            loadMoreJobPartitions={data.loadMoreJobPartitions}
          />
        </div>
      </div>
    </div>
  );
}
