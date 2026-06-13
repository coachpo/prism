import type { SettingsPageData } from "./useSettingsPageData";
import { AuthenticationSection } from "./sections/AuthenticationSection";
import { RetentionDeletionSection } from "./sections/RetentionDeletionSection";

interface SettingsGlobalTabProps {
  data: SettingsPageData;
}

export function SettingsGlobalTab({ data }: SettingsGlobalTabProps) {
  return (
    <div className="flex flex-col gap-5">
      <AuthenticationSection
        authSettings={data.authSettings}
        authEnabled={data.authEnabledInput}
        username={data.authUsername}
        setUsername={data.setAuthUsername}
        email={data.authEmail}
        setEmail={data.setAuthEmail}
        password={data.authPassword}
        passwordError={data.authPasswordError}
        setPassword={data.setAuthPassword}
        passwordConfirm={data.authPasswordConfirm}
        passwordMismatch={data.authPasswordMismatch}
        setPasswordConfirm={data.setAuthPasswordConfirm}
        emailVerificationOtp={data.emailVerificationOtp}
        setEmailVerificationOtp={data.setEmailVerificationOtp}
        sendingEmailVerification={data.sendingEmailVerification}
        confirmingEmailVerification={data.confirmingEmailVerification}
        onRequestEmailVerification={data.handleRequestEmailVerification}
        onConfirmEmailVerification={data.handleConfirmEmailVerification}
        authSaving={data.authSaving}
        onSaveAuthSettings={data.handleSaveAuthSettings}
      />

      <RetentionDeletionSection
        cleanupType={data.cleanupType}
        setCleanupType={data.setCleanupType}
        retentionPreset={data.retentionPreset}
        setRetentionPreset={data.setRetentionPreset}
        deleting={data.deleting}
        handleOpenDeleteConfirm={data.handleOpenDeleteConfirm}
        renderSectionSaveState={data.renderSaveStateForSection}
        handleSaveRetentionSettings={data.handleSaveRetentionSettings}
        retentionSettings={data.retentionSettings}
        retentionSettingsDirty={data.retentionSettingsDirty}
        retentionSettingsLoading={data.retentionSettingsLoading}
        retentionSettingsSaving={data.retentionSettingsSaving}
        setRetentionDays={data.setRetentionDays}
      />
    </div>
  );
}
