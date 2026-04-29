import { PageHeader } from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";
import { ProxyKeyDeleteAlertDialog } from "./proxy-api-keys/ProxyKeyDeleteAlertDialog";
import { ProxyKeyDetailSheet } from "./proxy-api-keys/ProxyKeyDetailSheet";
import { ProxyKeyEnforcementPanel } from "./proxy-api-keys/ProxyKeyEnforcementPanel";
import { ProxyKeyIssuePanel } from "./proxy-api-keys/ProxyKeyIssuePanel";
import { ProxyKeyLedgerCard } from "./proxy-api-keys/ProxyKeyLedgerCard";
import { ProxyApiKeysPageSkeleton } from "./proxy-api-keys/ProxyApiKeysPageSkeleton";
import { ProxyKeySecretReveal } from "./proxy-api-keys/ProxyKeySecretReveal";
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
    <div className="flex flex-col gap-6">
      <PageHeader title={copy.title} description={copy.description}>
        <Badge variant="outline" className={data.authStatusTone}>
          {authStatusLabel}
        </Badge>
      </PageHeader>

      {data.pageLoading ? (
        <ProxyApiKeysPageSkeleton />
      ) : (
        <div className="flex flex-col gap-6">
          <ProxyKeySecretReveal latestGeneratedKey={data.latestGeneratedKey} />

          <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <ProxyKeyIssuePanel
              authAvailable={Boolean(data.authSettings)}
              createDisabled={data.createDisabled}
              creatingProxyKey={data.creatingProxyKey}
              handleCreateSubmit={data.handleCreateSubmit}
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

            <ProxyKeyEnforcementPanel
              authEnabled={authEnabled}
              authSettings={data.authSettings}
              proxyKeyLimit={data.proxyKeyLimit}
              proxyKeysUsed={data.proxyKeys.length}
              remainingKeys={data.remainingKeys}
            />
          </div>

          <ProxyKeyLedgerCard
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
        </div>
      )}

      <ProxyKeyDetailSheet
        open={data.editProxyKeySheetOpen}
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

      <ProxyKeyDeleteAlertDialog
        authEnabled={authEnabled}
        open={data.deleteProxyKeyAlertOpen}
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
