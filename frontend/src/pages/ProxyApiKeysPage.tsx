import { PageHeader } from "@/components/PageHeader";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { DeleteProxyKeyDialog } from "./proxy-api-keys/DeleteProxyKeyDialog";
import { EditProxyKeyDialog } from "./proxy-api-keys/EditProxyKeyDialog";
import { ProxyApiKeysPageSkeleton } from "./proxy-api-keys/ProxyApiKeysPageSkeleton";
import { ProxyKeyCreateCard } from "./proxy-api-keys/ProxyKeyCreateCard";
import { ProxyKeysListCard } from "./proxy-api-keys/ProxyKeysListCard";
import { ProxyKeyStatusCallout } from "./proxy-api-keys/ProxyKeyStatusCallout";
import { useProxyApiKeysPageData } from "./proxy-api-keys/useProxyApiKeysPageData";

export function ProxyApiKeysPage() {
  const { messages } = useLocale();
  const data = useProxyApiKeysPageData();
  const copy = messages.proxyApiKeys;
  const authEnabled = Boolean(data.authSettings?.auth_enabled);
  const authStatusLabel = data.authSettings
    ? data.authSettings.auth_enabled
      ? copy.authenticationOn
      : copy.authenticationOff
    : copy.authenticationUnavailable;
  const deleteSuccessorId = data.deleteConfirm
    ? data.proxyKeySuccessorByParentId.get(data.deleteConfirm.id) ?? null
    : null;

  return (
    <div className="space-y-6">
      <PageHeader title={copy.title} description={copy.description}>
        <Badge variant="outline" className={data.authStatusTone}>
          {authStatusLabel}
        </Badge>
      </PageHeader>

      {data.pageLoading ? (
        <ProxyApiKeysPageSkeleton />
      ) : (
        <>
          <ProxyKeyCreateCard
            authAvailable={Boolean(data.authSettings)}
            createDisabled={data.createDisabled}
            creatingProxyKey={data.creatingProxyKey}
            handleCreateSubmit={data.handleCreateSubmit}
            latestGeneratedKey={data.latestGeneratedKey}
            proxyKeyExpiresAt={data.proxyKeyExpiresAt}
            proxyKeyLimit={data.proxyKeyLimit}
            proxyKeyName={data.proxyKeyName}
            proxyKeyNotes={data.proxyKeyNotes}
            proxyKeysUsed={data.proxyKeys.length}
            remainingKeys={data.remainingKeys}
            setProxyKeyExpiresAt={data.setProxyKeyExpiresAt}
            setProxyKeyName={data.setProxyKeyName}
            setProxyKeyNotes={data.setProxyKeyNotes}
          />

          <ProxyKeyStatusCallout authEnabled={authEnabled} />

          <ProxyKeysListCard
            authEnabled={authEnabled}
            deletingProxyKeyId={data.deletingProxyKeyId}
            displayedProxyKeys={data.displayedProxyKeys}
            onDelete={data.setDeleteConfirm}
            onEdit={data.startEditingProxyKey}
            onRotate={(keyId) => {
              void data.handleRotateProxyKey(keyId);
            }}
            proxyKeySuccessorByParentId={data.proxyKeySuccessorByParentId}
            rotatingProxyKeyId={data.rotatingProxyKeyId}
          />
        </>
      )}

      <EditProxyKeyDialog
        open={data.editProxyKeyDialogOpen}
        proxyKeyActive={data.editingProxyKeyActive}
        proxyKeyExpiresAt={data.editingProxyKeyExpiresAt}
        proxyKeyName={data.editingProxyKeyName}
        proxyKeyNotes={data.editingProxyKeyNotes}
        saving={data.savingEditedProxyKeyId !== null}
        onOpenChange={data.handleEditDialogOpenChange}
        onSubmit={data.handleEditSubmit}
        setProxyKeyActive={data.setEditingProxyKeyActive}
        setProxyKeyExpiresAt={data.setEditingProxyKeyExpiresAt}
        setProxyKeyName={data.setEditingProxyKeyName}
        setProxyKeyNotes={data.setEditingProxyKeyNotes}
      />

      <DeleteProxyKeyDialog
        authEnabled={authEnabled}
        open={data.deleteProxyKeyDialogOpen}
        deleteConfirm={data.deleteConfirm}
        displayedDeleteConfirm={data.displayedDeleteConfirm}
        deletingProxyKeyId={data.deletingProxyKeyId}
        onClose={() => data.setDeleteConfirm(null)}
        onDelete={() => void data.handleDeleteProxyKey()}
        onOpenChange={data.handleDeleteDialogOpenChange}
        successorId={deleteSuccessorId}
      />
    </div>
  );
}
