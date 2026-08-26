import { useBlocker } from "@tanstack/react-router"
import { useEffect } from "react"
import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Plus, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import {
  OperatorErrorState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
} from "@/shared/design-system"
import { api } from "@/lib/api"
import { ProxyKeyDeleteAlertDialog } from "@/pages/proxy-api-keys/ProxyKeyDeleteAlertDialog"
import { ProxyKeyDetailSheet } from "@/pages/proxy-api-keys/ProxyKeyDetailSheet"
import { ProxyKeyEnforcementPanel } from "@/pages/proxy-api-keys/ProxyKeyEnforcementPanel"
import { ProxyKeyIssuePanel } from "@/pages/proxy-api-keys/ProxyKeyIssuePanel"
import { ProxyKeyLedgerCard } from "@/pages/proxy-api-keys/ProxyKeyLedgerCard"
import { ProxyKeyRotateAlertDialog } from "@/pages/proxy-api-keys/ProxyKeyRotateAlertDialog"
import { ProxyKeyVerifyAccessDialog } from "@/pages/proxy-api-keys/ProxyKeyVerifyAccessDialog"
import { ProxyKeySecretDialog } from "./ProxyKeySecretDialog"
import { useProxyKeysFeatureData } from "./useProxyKeysFeatureData"
import { rewriteQueryKeys } from "@/shared/api/queryKeys"
import { isAuthSettingsEnabled } from "@/pages/proxy-api-keys/proxyKeyFormatting"

export default function ProxyKeysFeaturePage() {
  const { messages } = useLocale()
  const data = useProxyKeysFeatureData()
  const [verifyAccessOpen, setVerifyAccessOpen] = useState(false)
  const copy = messages.proxyApiKeys
  const authEnabled = isAuthSettingsEnabled(data.authSettings)

  // Models for the access panel (secret dialog and access verification).
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

  return (
    <OperatorPageShell data-testid="proxy-keys-feature-page">
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button type="button" variant="outline" onClick={() => setVerifyAccessOpen(true)}>
          <ShieldCheck data-icon="inline-start" />
          {copy.verifyAccess}
        </Button>
        <Button type="button" onClick={() => data.setIssueSheetOpen(true)}>
          <Plus data-icon="inline-start" />
          {copy.issueKey}
        </Button>
      </OperatorPageHeader>

      {data.pageError ? (
        <OperatorErrorState
          title={data.pageErrorTitle}
          description={data.pageError}
          action={<OperatorRetryButton onClick={data.retryPage}>{copy.retry}</OperatorRetryButton>}
        />
      ) : (
        <>
          <ProxyKeyEnforcementPanel authSettings={data.authSettings} loading={data.pageLoading} />

          <ProxyKeyLedgerCard
            authEnabled={authEnabled}
            capacity={data.capacity}
            deletingProxyKeyId={data.deletingProxyKeyId}
            displayedProxyKeys={data.displayedProxyKeys}
            loading={data.pageLoading}
            onDelete={data.setDeleteConfirm}
            onEdit={data.startEditingProxyKey}
            onIssue={() => data.setIssueSheetOpen(true)}
            onRetryUsage={data.retryUsage}
            onRotate={data.setRotateConfirm}
            onVisibleKeysChange={data.handleVisibleKeysChange}
            rotatingProxyKeyId={data.rotatingProxyKeyId}
            usage={data.usageEntries}
            usageFailed={data.usageFailed}
          />
        </>
      )}

      <ProxyKeyIssuePanel
        authAvailable={Boolean(data.authSettings)}
        capacity={data.capacity}
        createDisabled={data.createDisabled}
        creatingProxyKey={data.creatingProxyKey}
        handleCreateSubmit={data.handleCreateSubmit}
        onOpenChange={data.setIssueSheetOpen}
        open={data.issueSheetOpen}
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

      <ProxyKeyVerifyAccessDialog
        models={modelsQuery.data ?? []}
        modelsError={Boolean(modelsQuery.error)}
        modelsLoading={modelsQuery.isLoading}
        onOpenChange={setVerifyAccessOpen}
        onRetryModels={() => {
          void modelsQuery.refetch()
        }}
        open={verifyAccessOpen}
      />

      <ProxyKeySecretDialog
        key={data.secretSession.kind === "idle" ? "idle" : String(data.secretSession.session.keyId)}
        state={data.secretSession}
        models={modelsQuery.data ?? []}
        modelsError={Boolean(modelsQuery.error)}
        modelsLoading={modelsQuery.isLoading}
        onRequestClose={(intent) => data.dispatchSecretSession({ type: "REQUEST_CLOSE", intent })}
        onKeepEditing={() => data.dispatchSecretSession({ type: "KEEP_EDITING" })}
        onAbandonAndLeave={() => {
          data.dispatchSecretSession({ type: "ABANDON_AND_LEAVE" })
          blocker.reset?.()
        }}
        onRetryModels={() => {
          void modelsQuery.refetch()
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

      <ProxyKeyRotateAlertDialog
        authEnabled={authEnabled}
        open={data.rotateProxyKeyAlertOpen}
        rotateConfirm={data.rotateConfirm}
        displayedRotateConfirm={data.displayedRotateConfirm}
        rotating={data.rotatingProxyKeyId !== null}
        onCancel={() => data.setRotateConfirm(null)}
        onConfirm={() => void data.handleRotateProxyKey()}
        onOpenChange={data.handleRotateDialogOpenChange}
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
      />
    </OperatorPageShell>
  )
}
