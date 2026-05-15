import { useRef } from "react";
import { useLocale } from "@/i18n/useLocale";
import { AuthFilesTable } from "./AuthFilesTable";
import { DeleteSidecarDialog } from "./DeleteSidecarDialog";
import { ProviderInventoryTable } from "./ProviderInventoryTable";
import { QuotaInventoryPanel } from "./QuotaInventoryPanel";
import { SidecarActionHistory } from "./SidecarActionHistory";
import { SidecarDialog } from "./SidecarDialog";
import { SidecarsTable } from "./SidecarsTable";
import { WatchdogPolicyPanel } from "./WatchdogPolicyPanel";
import { useSidecarsPageData } from "./useSidecarsPageData";

export function SidecarsScaffold() {
  const { messages } = useLocale();
  const pageData = useSidecarsPageData();
  const copy = messages.sidecarsPage;
  const sidecarDetailRef = useRef<HTMLDivElement | null>(null);

  const handleSelectSidecar = (sidecarId: number) => {
    pageData.setSelectedSidecarId(sidecarId);
    window.requestAnimationFrame(() => {
      sidecarDetailRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };

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
        onSelect={handleSelectSidecar}
        onTestConnection={pageData.handleTestConnection}
        onManualSync={pageData.handleManualSync}
      />
      {pageData.selectedSidecar ? (
        <div ref={sidecarDetailRef} className="space-y-6" data-testid="sidecar-detail">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-semibold">{copy.detailTitle(pageData.selectedSidecar.name)}</h2>
            <p className="text-sm text-muted-foreground">{copy.detailDescription}</p>
          </div>
          <AuthFilesTable
            key={pageData.selectedSidecar.id}
            actionHistory={pageData.actionHistory}
            authSnapshots={pageData.authSnapshots}
            loading={pageData.sidecarDetailLoading}
            mutatingAuthKey={pageData.mutatingAuthKey}
            onPatchPriority={pageData.handlePatchAuthPriority}
            onPatchStatus={pageData.handlePatchAuthStatus}
            quotaStates={pageData.quotaStates}
          />
          <ProviderInventoryTable
            loading={pageData.sidecarDetailLoading}
            providerSnapshots={pageData.providerSnapshots}
          />
          <div className="space-y-6">
            <QuotaInventoryPanel
              loading={pageData.sidecarDetailLoading}
              mutating={pageData.quotaScanMutating}
              onCancelScan={pageData.handleCancelQuotaScan}
              onStartScan={pageData.handleStartQuotaScan}
              scans={pageData.quotaScans}
            />
            <WatchdogPolicyPanel
              applying={pageData.watchdogPolicyApplying}
              loading={pageData.sidecarDetailLoading}
              onApply={pageData.handleApplyWatchdogPolicy}
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
