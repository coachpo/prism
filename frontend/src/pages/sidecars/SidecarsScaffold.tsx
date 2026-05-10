import { AuthFilesTable } from "./AuthFilesTable";
import { DeleteSidecarDialog } from "./DeleteSidecarDialog";
import { ProviderInventoryTable } from "./ProviderInventoryTable";
import { SidecarActionHistory } from "./SidecarActionHistory";
import { SidecarDialog } from "./SidecarDialog";
import { SidecarsTable } from "./SidecarsTable";
import { WatchdogPolicyPanel } from "./WatchdogPolicyPanel";
import { useSidecarsPageData } from "./useSidecarsPageData";

export function SidecarsScaffold() {
  const pageData = useSidecarsPageData();

  return (
    <>
      <SidecarsTable
        sidecars={pageData.sidecars}
        sidecarsLoading={pageData.sidecarsLoading}
        preparingEditSidecarId={pageData.preparingEditSidecarId}
        testingSidecarId={pageData.testingSidecarId}
        syncingSidecarId={pageData.syncingSidecarId}
        selectedSidecarId={pageData.selectedSidecarId}
        onCreate={pageData.openCreateSidecarDialog}
        onEdit={pageData.handleEditSidecar}
        onDelete={pageData.openDeleteSidecarDialog}
        onSelect={pageData.setSelectedSidecarId}
        onTestConnection={pageData.handleTestConnection}
        onManualSync={pageData.handleManualSync}
      />
      {pageData.selectedSidecar ? (
        <div className="space-y-6" data-testid="sidecar-detail">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-semibold">{pageData.selectedSidecar.name} detail</h2>
            <p className="text-sm text-muted-foreground">
              Auth inventory, provider inventory, watchdog policy, and action history for the selected sidecar.
            </p>
          </div>
          <AuthFilesTable
            key={pageData.selectedSidecar.id}
            actionHistory={pageData.actionHistory}
            authSnapshots={pageData.authSnapshots}
            loading={pageData.sidecarDetailLoading}
            mutatingAuthKey={pageData.mutatingAuthKey}
            onPatchPriority={pageData.handlePatchAuthPriority}
            onPatchStatus={pageData.handlePatchAuthStatus}
          />
          <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.7fr)]">
            <ProviderInventoryTable
              loading={pageData.sidecarDetailLoading}
              providerSnapshots={pageData.providerSnapshots}
            />
            <WatchdogPolicyPanel
              loading={pageData.sidecarDetailLoading}
              onSave={pageData.handleSaveWatchdogPolicy}
              policy={pageData.watchdogPolicy}
              saving={pageData.watchdogPolicySaving}
            />
          </div>
          <SidecarActionHistory actions={pageData.actionHistory} loading={pageData.sidecarDetailLoading} />
        </div>
      ) : null}
      <SidecarDialog
        open={pageData.sidecarDialogOpen}
        editingSidecar={pageData.editingSidecar}
        sidecarForm={pageData.sidecarForm}
        sidecarSaving={pageData.sidecarSaving}
        setSidecarForm={pageData.setSidecarForm}
        onClose={pageData.closeSidecarDialog}
        onOpenChange={(open) => {
          if (!open) {
            pageData.closeSidecarDialog();
          }
        }}
        onSave={pageData.handleSaveSidecar}
      />
      <DeleteSidecarDialog
        open={pageData.deleteSidecarDialogOpen}
        deleteSidecarConfirm={pageData.deleteSidecarConfirm}
        displayedDeleteSidecarConfirm={pageData.displayedDeleteSidecarConfirm}
        sidecarDeleting={pageData.sidecarDeleting}
        onClose={pageData.closeDeleteSidecarDialog}
        onDelete={pageData.handleDeleteSidecar}
      />
    </>
  );
}
