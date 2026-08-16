import type { ComponentProps } from "react"
import { useCallback, useMemo, useReducer, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { getStaticMessages } from "@/i18n/staticMessages"
import { api } from "@/lib/api"
import type { ProxyApiKey, ProxyApiKeyUpdate, ProxyKeyCapacity } from "@/lib/types"
import { rewriteQueryKeys } from "@/shared/api/queryKeys"
import type { ResolvedExpiryInput } from "@/pages/proxy-api-keys/ProxyKeyExpiryField"
import { generatedProxyKeyInitialState, generatedProxyKeyReducer } from "./generatedSecretSession"
import { useProxyKeyUsage } from "./useProxyKeyUsage"

type FormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0]

export function useProxyKeysFeatureData() {
  const queryClient = useQueryClient()
  const messages = getStaticMessages()
  const [proxyKeyName, setProxyKeyName] = useState("")
  const [proxyKeyNotes, setProxyKeyNotes] = useState("")
  const [proxyKeyExpiresAt, setProxyKeyExpiresAt] = useState("")
  const [proxyKeyExpiresResolved, setProxyKeyExpiresResolved] = useState<ResolvedExpiryInput | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<ProxyApiKey | null>(null)
  const [deleteProxyKeyAlertOpen, setDeleteProxyKeyAlertOpen] = useState(false)
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] = useState<ProxyApiKey | null>(null)
  const [rotateConfirm, setRotateConfirm] = useState<ProxyApiKey | null>(null)
  const [rotateProxyKeyAlertOpen, setRotateProxyKeyAlertOpen] = useState(false)
  const [displayedRotateConfirm, setDisplayedRotateConfirm] = useState<ProxyApiKey | null>(null)
  const [issueSheetOpen, setIssueSheetOpen] = useState(false)
  const [verifyAccessOpen, setVerifyAccessOpen] = useState(false)
  const [visibleKeyIds, setVisibleKeyIds] = useState<number[]>([])
  const [editingProxyKey, setEditingProxyKey] = useState<ProxyApiKey | null>(null)
  const [editProxyKeySheetOpen, setEditProxyKeySheetOpen] = useState(false)
  const [editingProxyKeyName, setEditingProxyKeyName] = useState("")
  const [editingProxyKeyNotes, setEditingProxyKeyNotes] = useState("")
  const [editingProxyKeyExpiresAt, setEditingProxyKeyExpiresAt] = useState("")
  const [editingProxyKeyExpiresResolved, setEditingProxyKeyExpiresResolved] = useState<ResolvedExpiryInput | null>(null)
  const [editingProxyKeyActive, setEditingProxyKeyActive] = useState(false)
  const [secretSession, dispatchSecretSession] = useReducer(
    generatedProxyKeyReducer,
    generatedProxyKeyInitialState,
  )

  const authSettingsQuery = useQuery({
    queryKey: rewriteQueryKeys.global.settingsAuth(),
    queryFn: api.settings.auth.get,
  })
  const proxyKeysQuery = useQuery({
    queryKey: rewriteQueryKeys.global.proxyApiKeys(),
    queryFn: api.settings.auth.proxyKeys.list,
  })

  const authSettings = authSettingsQuery.data ?? null
  const proxyKeyList = proxyKeysQuery.data
  // The ledger is server-backed; capacity comes from the authoritative
  // snapshot, never from list length.
  const proxyKeys = useMemo(() => proxyKeyList?.items ?? [], [proxyKeyList])
  const capacity = proxyKeyList?.capacity ?? null
  const proxyKeyLimit = capacity?.limit ?? authSettings?.proxy_key_limit ?? 100
  // Never infer capacity from the currently loaded list: a stale/failed list
  // query is not evidence that slots are available. Mutations remain disabled
  // until the server has supplied an authoritative snapshot.
  const remainingKeys = capacity?.remaining ?? 0

  const pageLoading = authSettingsQuery.isLoading || proxyKeysQuery.isLoading
  const pageError = authSettingsQuery.error || proxyKeysQuery.error
  // The page-level error names the read that actually failed, so a broken key
  // list is never reported as an authentication problem.
  const pageErrorTitle = proxyKeysQuery.error
    ? messages.proxyApiKeysData.loadKeysFailed
    : messages.proxyApiKeysData.loadAuthStatusFailed
  const retryPage = useCallback(() => {
    void Promise.all([authSettingsQuery.refetch(), proxyKeysQuery.refetch()])
  }, [authSettingsQuery, proxyKeysQuery])

  const usage = useProxyKeyUsage(visibleKeyIds)
  const handleVisibleKeysChange = useCallback((keyIds: number[]) => {
    setVisibleKeyIds((current) =>
      current.length === keyIds.length && current.every((id, index) => id === keyIds[index])
        ? current
        : keyIds,
    )
  }, [])

  const createMutation = useMutation({ mutationFn: api.settings.auth.proxyKeys.create })
  const rotateMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.rotate(keyId) })
  const updateMutation = useMutation({
    mutationFn: ({ keyId, payload }: { keyId: number; payload: ProxyApiKeyUpdate }) =>
      api.settings.auth.proxyKeys.update(keyId, payload),
  })
  const deleteMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.delete(keyId) })

  const createDisabled = createMutation.isPending || !authSettings || !capacity || remainingKeys === 0

  const reconcileLedgerFromMutation = useCallback(
    (item: ProxyApiKey, nextCapacity: ProxyKeyCapacity) => {
      queryClient.setQueryData<ProxyApiKeyListData>(rewriteQueryKeys.global.proxyApiKeys(), (current) => ({
        items: [item, ...(current?.items ?? []).filter((existing) => existing.id !== item.id)],
        capacity: nextCapacity,
      }))
      void queryClient.invalidateQueries({ queryKey: rewriteQueryKeys.global.proxyApiKeys() })
    },
    [queryClient],
  )

  async function handleCreateProxyKey() {
    if (!authSettings) {
      toast.error(messages.proxyApiKeysData.settingsUnavailable)
      return
    }
    if (!proxyKeyName.trim()) {
      toast.error(messages.proxyApiKeysData.keyNameRequired)
      return
    }
    if (remainingKeys <= 0) {
      toast.error(messages.proxyApiKeysData.maxKeysReached(String(proxyKeyLimit)))
      return
    }

    try {
      const created = await createMutation.mutateAsync({
        name: proxyKeyName.trim(),
        notes: proxyKeyNotes.trim() || null,
        expires_at:
          proxyKeyExpiresResolved && !proxyKeyExpiresResolved.preserved && !proxyKeyExpiresResolved.gapError
            ? proxyKeyExpiresResolved.instant
            : normalizeExpiresAtInput(proxyKeyExpiresAt),
      })
      // The raw key is consumed into the unacknowledged session before any
      // query invalidation, notification or render branch can run.
      dispatchSecretSession({
        type: "CREATE_SUCCEEDED",
        session: {
          source: "create",
          keyId: created.item.id,
          itemSnapshot: created.item,
          rawKey: created.key,
          capacity: created.capacity,
          openedAt: Date.now(),
          savedAcknowledged: false,
        },
      })
      // The raw key now lives only in the unacknowledged session; clear the
      // mutation-owned response data so no second reference survives.
      createMutation.reset()
      setProxyKeyName("")
      setProxyKeyNotes("")
      setProxyKeyExpiresAt("")
      setProxyKeyExpiresResolved(null)
      setIssueSheetOpen(false)
      reconcileLedgerFromMutation(created.item, created.capacity)
      toast.success(messages.proxyApiKeysData.created)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.createFailed)
    }
  }

  // Rotation invalidates the live credential, so it only runs from the
  // confirmation dialog — never straight from a row icon.
  async function handleRotateProxyKey() {
    if (!rotateConfirm) return
    const keyId = rotateConfirm.id
    try {
      const rotated = await rotateMutation.mutateAsync(keyId)
      dispatchSecretSession({
        type: "ROTATE_SUCCEEDED",
        session: {
          source: "rotate",
          keyId: rotated.item.id,
          itemSnapshot: rotated.item,
          rawKey: rotated.key,
          capacity: rotated.capacity,
          openedAt: Date.now(),
          savedAcknowledged: false,
        },
      })
      rotateMutation.reset()
      setRotateProxyKeyAlertOpen(false)
      setRotateConfirm(null)
      // Rotation is in-place: the row keeps its id, so reconciling the returned
      // snapshot is the entire ledger update. There is no predecessor to retire.
      reconcileLedgerFromMutation(rotated.item, rotated.capacity)
      toast.success(messages.proxyApiKeysData.rotated)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.rotateFailed)
    }
  }

  async function handleDeleteProxyKey() {
    if (!deleteConfirm) return
    const deletingKey = deleteConfirm

    try {
      const deleted = await deleteMutation.mutateAsync(deletingKey.id)
      queryClient.setQueryData<ProxyApiKeyListData>(rewriteQueryKeys.global.proxyApiKeys(), (current) => ({
        items: (current?.items ?? []).filter((key) => key.id !== deletingKey.id),
        capacity: deleted.capacity,
      }))
      void queryClient.invalidateQueries({ queryKey: rewriteQueryKeys.global.proxyApiKeys() })
      setDeleteProxyKeyAlertOpen(false)
      setDeleteConfirm(null)
      toast.success(messages.proxyApiKeysData.deleted)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.deleteFailed)
    }
  }

  const startEditingProxyKey = (item: ProxyApiKey) => {
    setEditingProxyKey(item)
    setEditingProxyKeyName(item.name)
    setEditingProxyKeyNotes(item.notes ?? "")
    setEditingProxyKeyExpiresAt(item.expires_at ?? "")
    setEditingProxyKeyExpiresResolved(null)
    setEditingProxyKeyActive(item.is_active)
    setEditProxyKeySheetOpen(true)
  }

  async function handleSaveEditedProxyKey() {
    if (!editingProxyKey) return
    const nextName = editingProxyKeyName.trim()
    if (!nextName) {
      toast.error(messages.proxyApiKeysData.keyNameRequired)
      return
    }

    try {
      const payload: ProxyApiKeyUpdate = {
        name: nextName,
        notes: editingProxyKeyNotes.trim() || null,
        is_active: editingProxyKeyActive,
        ...editingExpiryPayload(editingProxyKeyExpiresResolved, editingProxyKeyExpiresAt),
      }
      const updated = await updateMutation.mutateAsync({ keyId: editingProxyKey.id, payload })
      queryClient.setQueryData<ProxyApiKeyListData>(rewriteQueryKeys.global.proxyApiKeys(), (current) => ({
        items: (current?.items ?? []).map((key) => (key.id === updated.item.id ? updated.item : key)),
        capacity: updated.capacity,
      }))
      setEditingProxyKey(updated.item)
      setEditingProxyKeyName(updated.item.name)
      setEditingProxyKeyNotes(updated.item.notes ?? "")
      setEditingProxyKeyExpiresAt(updated.item.expires_at ?? "")
      setEditingProxyKeyExpiresResolved(null)
      setEditingProxyKeyActive(updated.item.is_active)
      setEditProxyKeySheetOpen(false)
      toast.success(messages.proxyApiKeysData.updated)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.updateFailed)
    }
  }

  const handleCreateSubmit = (event: FormSubmitEvent) => {
    event.preventDefault()
    void handleCreateProxyKey()
  }

  const handleEditSubmit = (event: FormSubmitEvent) => {
    event.preventDefault()
    void handleSaveEditedProxyKey()
  }

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (!open && !deleteMutation.isPending) {
      setDeleteProxyKeyAlertOpen(false)
      setDeleteConfirm(null)
      return
    }
    setDeleteProxyKeyAlertOpen(open)
  }

  const handleEditDialogOpenChange = (open: boolean) => {
    if (!open && !updateMutation.isPending) {
      setEditProxyKeySheetOpen(false)
      return
    }
    setEditProxyKeySheetOpen(open)
  }

  const setDeleteConfirmState = (item: ProxyApiKey | null) => {
    setDeleteConfirm(item)
    if (item) {
      setDisplayedDeleteConfirm(item)
      setDeleteProxyKeyAlertOpen(true)
      return
    }
    setDeleteProxyKeyAlertOpen(false)
  }

  const setRotateConfirmState = (item: ProxyApiKey | null) => {
    setRotateConfirm(item)
    if (item) {
      setDisplayedRotateConfirm(item)
      setRotateProxyKeyAlertOpen(true)
      return
    }
    setRotateProxyKeyAlertOpen(false)
  }

  const handleRotateDialogOpenChange = (open: boolean) => {
    if (!open && !rotateMutation.isPending) {
      setRotateProxyKeyAlertOpen(false)
      setRotateConfirm(null)
      return
    }
    setRotateProxyKeyAlertOpen(open)
  }

  return {
    authSettings,
    capacity,
    createDisabled,
    creatingProxyKey: createMutation.isPending,
    deleteConfirm,
    deleteProxyKeyAlertOpen,
    deletingProxyKeyId: deleteMutation.isPending ? deleteMutation.variables ?? null : null,
    displayedDeleteConfirm,
    dispatchSecretSession,
    editProxyKeySheetOpen,
    editingProxyKey,
    editingProxyKeyActive,
    editingProxyKeyExpiresAt,
    editingProxyKeyExpiresResolved,
    editingProxyKeyName,
    editingProxyKeyNotes,
    displayedProxyKeys: [...proxyKeys].sort((left, right) => right.id - left.id),
    handleCreateSubmit,
    handleDeleteDialogOpenChange,
    handleDeleteProxyKey,
    handleEditDialogOpenChange,
    handleEditSubmit,
    handleRotateProxyKey,
    handleRotateDialogOpenChange,
    handleVisibleKeysChange,
    issueSheetOpen,
    setIssueSheetOpen,
    verifyAccessOpen,
    setVerifyAccessOpen,
    displayedRotateConfirm,
    rotateConfirm,
    rotateProxyKeyAlertOpen,
    setRotateConfirm: setRotateConfirmState,
    usageEntries: usage.entries,
    usageFailed: usage.hasFailure,
    retryUsage: usage.refetch,
    pageError: pageError instanceof Error ? pageError.message : pageError ? messages.proxyApiKeysData.loadKeysFailed : null,
    pageErrorTitle,
    pageLoading,
    retryPage,
    proxyKeyExpiresAt,
    proxyKeyLimit,
    proxyKeyName,
    proxyKeyNotes,
    proxyKeys,
    remainingKeys,
    rotatingProxyKeyId: rotateMutation.isPending ? rotateMutation.variables ?? null : null,
    savingEditedProxyKeyId: updateMutation.isPending ? updateMutation.variables?.keyId ?? null : null,
    secretSession,
    setDeleteConfirm: setDeleteConfirmState,
    setEditingProxyKeyActive,
    setEditingProxyKeyExpiresAt,
    setEditingProxyKeyExpiresResolved,
    setEditingProxyKeyName,
    setEditingProxyKeyNotes,
    proxyKeyExpiresResolved,
    setProxyKeyExpiresAt,
    setProxyKeyExpiresResolved,
    setProxyKeyName,
    setProxyKeyNotes,
    startEditingProxyKey,
  }
}

type ProxyApiKeyListData = { items: ProxyApiKey[]; capacity: ProxyKeyCapacity }

function editingExpiryPayload(
  resolved: ResolvedExpiryInput | null,
  wallClock: string,
): Pick<ProxyApiKeyUpdate, "expires_at"> {
  if (resolved && resolved.preserved) {
    // Omit: preserve the current value.
    return {}
  }
  if (resolved && resolved.gapError) {
    return { expires_at: undefined }
  }
  if (resolved && resolved.instant === null) {
    // Explicit clear.
    return { expires_at: null }
  }
  if (resolved && resolved.instant !== null) {
    return { expires_at: resolved.instant }
  }
  // No resolved value yet: fall back to the wall-clock string if present.
  const trimmed = wallClock.trim()
  if (trimmed === "") {
    return {}
  }
  return { expires_at: trimmed }
}

function normalizeExpiresAtInput(value: string): string | null | undefined {
  const trimmed = value.trim()
  if (trimmed === "") {
    return null
  }
  return trimmed
}
