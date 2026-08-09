import { useBlocker } from "@tanstack/react-router"
import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/i18n/useLocale"
import { OperatorPageHeader } from "@/shared/design-system"
import { api } from "@/lib/api"
import { ProxyKeyDeleteAlertDialog } from "@/pages/proxy-api-keys/ProxyKeyDeleteAlertDialog"
import { ProxyKeyDetailSheet } from "@/pages/proxy-api-keys/ProxyKeyDetailSheet"
import { ProxyKeyEnforcementPanel } from "@/pages/proxy-api-keys/ProxyKeyEnforcementPanel"
import { ProxyKeyIssuePanel } from "@/pages/proxy-api-keys/ProxyKeyIssuePanel"
import { ProxyKeyLedgerCard } from "@/pages/proxy-api-keys/ProxyKeyLedgerCard"
import { ProxyApiKeysPageSkeleton } from "@/pages/proxy-api-keys/ProxyApiKeysPageSkeleton"
import { ProxyKeySecretDialog } from "./ProxyKeySecretDialog"
import { useProxyKeysFeatureData } from "./useProxyKeysFeatureData"
import { rewriteQueryKeys } from "@/shared/api/queryKeys"

export default function ProxyKeysFeaturePage() {
  const { messages } = useLocale()
  const data = useProxyKeysFeatureData()
  const copy = messages.proxyApiKeys
  const authEnabled = Boolean(data.authSettings?.auth_enabled)
  const authStatusLabel = data.authSettings
    ? data.authSettings.auth_enabled
      ? copy.authenticationOn
      : copy.authenticationOff
    : copy.authenticationUnavailable
  const deleteSuccessorId = data.deleteConfirm
    ? data.proxyKeySuccessorByParentId.get(data.deleteConfirm.id) ?? null
    : null

  // Models for the access panel (secret dialog model/operation selector).
  // Model loading/error must never affect the secret session.
  const modelsQuery = useQuery({
    queryKey: rewriteQueryKeys.global.models(),
    queryFn: api.models.list,
  })

  // SPA navigation is blocked while the raw key is unacknowledged. The
  // blocker offers keep-editing or explicit abandon; beforeunload covers
  // refresh/tab close with the native prompt.
  const unacknowledged = data.secretSession.kind === "unacknowledged"
  const blocker = useBlocker({
    condition: unacknowledged,
    blockerFn: () => {
      data.dispatchSecretSession({ type: "REQUEST_CLOSE", intent: "navigate" })
      return { status: "blocked" }
    },
  })

  useEffect(() => {
    if (!unacknowledged) {
      return
    }
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ""
    }
    window.addEventListener("beforeunload", handleBeforeUnload)
    return () => window.removeEventListener("beforeunload", handleBeforeUnload)
  }, [unacknowledged])

  const secretState = data.secretSession
  const handleSecretRequestClose = (intent: "close" | "navigate") => {
    data.dispatchSecretSession({ type: "REQUEST_CLOSE", intent })
  }

  return (
    <div className="flex flex-col gap-6">
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Badge variant="outline" className={data.authStatusTone}>
          {authStatusLabel}
        </Badge>
      </OperatorPageHeader>

      {data.pageLoading ? (
        <ProxyApiKeysPageSkeleton />
      ) : (
        <div className="flex flex-col gap-6">
          <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <ProxyKeyIssuePanel
              authAvailable={Boolean(data.authSettings)}
              capacity={data.capacity}
              createDisabled={data.createDisabled}
              creatingProxyKey={data.creatingProxyKey}
              handleCreateSubmit={data.handleCreateSubmit}
              proxyKeyExpiresAt={data.proxyKeyExpiresAt}
              proxyKeyExpiresResolved={data.proxyKeyExpiresResolved}
              proxyKeyLimit={data.proxyKeyLimit}
              proxyKeyName={data.proxyKeyName}
              proxyKeyNotes={data.proxyKeyNotes}
              remainingKeys={data.remainingKeys}
              setProxyKeyExpiresAt={data.setProxyKeyExpiresAt}
              setProxyKeyExpiresResolved={data.setProxyKeyExpiresResolved}
              setProxyKeyName={data.setProxyKeyName}
              setProxyKeyNotes={data.setProxyKeyNotes}
            />

            <ProxyKeyEnforcementPanel
              authEnabled={authEnabled}
              authSettings={data.authSettings}
            />
          </div>

          <ProxyKeyLedgerCard
            authEnabled={authEnabled}
            deletingProxyKeyId={data.deletingProxyKeyId}
            displayedProxyKeys={data.displayedProxyKeys}
            onDelete={data.setDeleteConfirm}
            onEdit={data.startEditingProxyKey}
            onRotate={(keyId) => {
              void data.handleRotateProxyKey(keyId)
            }}
            proxyKeySuccessorByParentId={data.proxyKeySuccessorByParentId}
            rotatingProxyKeyId={data.rotatingProxyKeyId}
          />
        </div>
      )}

      <ProxyKeySecretDialog
        key={secretState.kind === "idle" ? "idle" : String(secretState.session.keyId)}
        state={secretState}
        models={modelsQuery.data ?? []}
        modelsError={Boolean(modelsQuery.error)}
        modelsLoading={modelsQuery.isLoading}
        onRequestClose={handleSecretRequestClose}
        onKeepEditing={() => data.dispatchSecretSession({ type: "KEEP_EDITING" })}
        onAbandonAndLeave={() => {
          data.dispatchSecretSession({ type: "ABANDON_AND_LEAVE" })
          blocker.reset?.()
        }}
        onSetSavedAck={(acknowledged) => data.dispatchSecretSession({ type: "SET_SAVED_ACK", acknowledged })}
        onFinish={() => data.dispatchSecretSession({ type: "FINISH" })}
      />

      <ProxyKeyDetailSheet
        open={data.editProxyKeySheetOpen}
        proxyKeyActive={data.editingProxyKeyActive}
        proxyKeyExpiresAt={data.editingProxyKeyExpiresAt}
        proxyKeyExpiresResolved={data.editingProxyKeyExpiresResolved}
        proxyKeyName={data.editingProxyKeyName}
        proxyKeyNotes={data.editingProxyKeyNotes}
        saving={data.savingEditedProxyKeyId !== null}
        onOpenChange={data.handleEditDialogOpenChange}
        onSubmit={data.handleEditSubmit}
        setProxyKeyActive={data.setEditingProxyKeyActive}
        setProxyKeyExpiresAt={data.setEditingProxyKeyExpiresAt}
        setProxyKeyExpiresResolved={data.setEditingProxyKeyExpiresResolved}
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
  )
}
