import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";
import type { SettingsPageData } from "./useSettingsPageData";
import { DeleteVendorDialog } from "./dialogs/DeleteVendorDialog";
import { VendorDialog } from "./dialogs/VendorDialog";
import { AuthenticationSection } from "./sections/AuthenticationSection";
import { VendorManagementSection } from "./sections/VendorManagementSection";

interface SettingsGlobalTabProps {
  data: SettingsPageData;
}

export function SettingsGlobalTab({ data }: SettingsGlobalTabProps) {
  const { messages } = useLocale();

  return (
    <div className="space-y-6">
      <div className="rounded-lg border border-blue-500/25 bg-blue-500/10 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <Badge
            variant="outline"
            className="w-fit border-blue-500/30 bg-blue-500/15 text-blue-700 dark:text-blue-300"
          >
            {messages.settingsPage.globalSettings}
          </Badge>
          <p className="text-sm text-blue-800 dark:text-blue-300">
            {messages.settingsPage.globalSettingsDescription}
          </p>
        </div>
      </div>

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

      <VendorManagementSection
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
