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
  const authStatusLabel = data.authSettings
    ? data.authSettings.auth_enabled
      ? copy.authenticationOn
      : copy.authenticationOff
    : copy.authenticationUnavailable;

  return (
    <div className="space-y-6">
      <PageHeader
        title={copy.title}
        description={copy.description}
      >
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
            proxyKeyLimit={data.proxyKeyLimit}
            proxyKeyName={data.proxyKeyName}
            proxyKeyNotes={data.proxyKeyNotes}
            proxyKeysUsed={data.proxyKeys.length}
            remainingKeys={data.remainingKeys}
            setProxyKeyName={data.setProxyKeyName}
            setProxyKeyNotes={data.setProxyKeyNotes}
          />

          <ProxyKeyStatusCallout authEnabled={Boolean(data.authSettings?.auth_enabled)} />

          <ProxyKeysListCard
            displayedProxyKeys={data.displayedProxyKeys}
            authEnabled={Boolean(data.authSettings?.auth_enabled)}
            rotatingProxyKeyId={data.rotatingProxyKeyId}
            deletingProxyKeyId={data.deletingProxyKeyId}
            onRotate={(keyId) => {
              void data.handleRotateProxyKey(keyId);
            }}
            onEdit={data.startEditingProxyKey}
            onDelete={data.setDeleteConfirm}
          />
        </>
      )}

      <EditProxyKeyDialog
        open={data.editProxyKeyDialogOpen}
        proxyKeyActive={data.editingProxyKeyActive}
        proxyKeyName={data.editingProxyKeyName}
        proxyKeyNotes={data.editingProxyKeyNotes}
        saving={data.savingEditedProxyKeyId !== null}
        onOpenChange={data.handleEditDialogOpenChange}
        onSubmit={data.handleEditSubmit}
        setProxyKeyActive={data.setEditingProxyKeyActive}
        setProxyKeyName={data.setEditingProxyKeyName}
        setProxyKeyNotes={data.setEditingProxyKeyNotes}
      />

      <DeleteProxyKeyDialog
        open={data.deleteProxyKeyDialogOpen}
        deleteConfirm={data.deleteConfirm}
        displayedDeleteConfirm={data.displayedDeleteConfirm}
        deletingProxyKeyId={data.deletingProxyKeyId}
        onClose={() => data.setDeleteConfirm(null)}
        onDelete={() => void data.handleDeleteProxyKey()}
        onOpenChange={data.handleDeleteDialogOpenChange}
      />
    </div>
  );
}
