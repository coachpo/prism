import type { SettingsPageData } from "./useSettingsPageData";
import { DeleteVendorDialog } from "./dialogs/DeleteVendorDialog";
import { VendorDialog } from "./dialogs/VendorDialog";
import { AuthenticationSection } from "./sections/AuthenticationSection";
import { RetentionDeletionSection } from "./sections/RetentionDeletionSection";
import { VendorManagementSection } from "./sections/VendorManagementSection";

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

      <VendorManagementSection
        catalogExporting={data.catalogExporting}
        catalogFileInputRef={data.catalogFileInputRef}
        catalogImporting={data.catalogImporting}
        catalogImportSummary={data.catalogImportSummary}
        catalogParsedImport={data.catalogParsedImport}
        catalogPreviewing={data.catalogPreviewing}
        catalogPreviewInvalidationReason={data.catalogPreviewInvalidationReason}
        catalogPreviewReadyForSelection={data.catalogPreviewReadyForSelection}
        catalogPreviewResult={data.catalogPreviewResult}
        catalogSelectedFile={data.catalogSelectedFile}
        handleCatalogExport={data.handleCatalogExport}
        handleCatalogFileSelect={data.handleCatalogFileSelect}
        handleCatalogImport={data.handleCatalogImport}
        handleCatalogPreview={data.handleCatalogPreview}
        vendors={data.vendors}
        vendorsLoading={data.vendorsLoading}
        onCreateVendor={data.openCreateVendorDialog}
        onEditVendor={data.handleEditVendor}
        onDeleteVendor={data.handleDeleteVendorClick}
      />

      <VendorDialog
        open={data.vendorDialogOpen}
        onClose={data.closeVendorDialog}
        editingVendor={data.editingVendor}
        vendorForm={data.vendorForm}
        setVendorForm={data.setVendorForm}
        onSave={data.handleSaveVendor}
        vendorSaving={data.vendorSaving}
      />

      <DeleteVendorDialog
        deleteVendorConfirm={data.deleteVendorConfirm}
        deleteVendorConflict={data.deleteVendorConflict}
        displayedDeleteVendorConfirm={data.displayedDeleteVendorConfirm}
        onClose={data.closeDeleteVendorDialog}
        onDelete={data.handleDeleteVendor}
        open={data.deleteVendorDialogOpen}
        vendorDeleting={data.vendorDeleting}
        vendorUsageLoading={data.vendorUsageLoading}
        vendorUsageRows={data.vendorUsageRows}
      />
    </div>
  );
}
