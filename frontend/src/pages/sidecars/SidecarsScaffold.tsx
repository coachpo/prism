import { useRef } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { AuthFilesTable } from "./AuthFilesTable";
import { DeleteSidecarDialog } from "./DeleteSidecarDialog";
import { ProviderInventoryTable } from "./ProviderInventoryTable";
import { SidecarDialog } from "./SidecarDialog";
import { SidecarsTable } from "./SidecarsTable";
import { useSidecarsPageData } from "./useSidecarsPageData";

export function SidecarsScaffold() {
  const { messages } = useLocale();
  const pageData = useSidecarsPageData();
  const copy = messages.sidecarsPage;
  const sidecarDetailRef = useRef<HTMLDivElement | null>(null);
  const selectedSidecar = pageData.selectedSidecar;

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
      {selectedSidecar ? (
        <div ref={sidecarDetailRef} className="space-y-6" data-testid="sidecar-detail">
          <div className="flex flex-col gap-1">
            <h2 className="text-base font-semibold">{copy.detailTitle(selectedSidecar.name)}</h2>
            <p className="text-sm text-muted-foreground">{copy.tableDescription}</p>
          </div>
          {pageData.sidecarDetailRefreshError ? (
            <div className="flex items-center justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-900 dark:text-amber-100">
              <span>{pageData.sidecarDetailRefreshError}</span>
              <Button type="button" variant="outline" size="sm" onClick={() => void pageData.refreshSidecarDetail(selectedSidecar.id)}>
                Retry
              </Button>
            </div>
          ) : null}
          <AuthFilesTable
            key={selectedSidecar.id}
            authSnapshots={pageData.authSnapshots}
            authMutationNotices={pageData.authMutationNotices}
            loading={pageData.sidecarDetailLoading}
            mutatingAuthKey={pageData.mutatingAuthKey}
            onDeleteAuthFile={pageData.handleDeleteAuthFile}
            onLoadModels={pageData.handleLoadAuthModels}
            onPatchPriority={pageData.handlePatchAuthPriority}
            onPatchStatus={pageData.handlePatchAuthStatus}
          />
          <ProviderInventoryTable
            loading={pageData.sidecarDetailLoading}
            providerSnapshots={pageData.providerSnapshots}
          />
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
