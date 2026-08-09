import type { ComponentProps } from "react"
import { useCallback, useEffect, useMemo, useReducer, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { getStaticMessages } from "@/i18n/staticMessages"
import { api } from "@/lib/api"
import type { ProxyApiKey, ProxyApiKeyUpdate, ProxyKeyCapacity } from "@/lib/types"
import { rewriteQueryKeys } from "@/shared/api/queryKeys"
import { getAuthStatusTone } from "@/pages/proxy-api-keys/proxyKeyFormatting"
import type { ResolvedExpiryInput } from "@/pages/proxy-api-keys/ProxyKeyExpiryField"
import { generatedProxyKeyInitialState, generatedProxyKeyReducer } from "./generatedSecretSession"

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
  const remainingKeys = capacity?.remaining ?? (authSettings ? Math.max(proxyKeyLimit - proxyKeys.length, 0) : 0)

  const authStatusLabel = authSettings
    ? authSettings.auth_enabled
      ? messages.proxyApiKeys.authenticationOn
      : messages.proxyApiKeys.authenticationOff
    : messages.proxyApiKeys.authenticationUnavailable
  const authStatusTone = getAuthStatusTone(authSettings)
  const pageLoading = authSettingsQuery.isLoading || proxyKeysQuery.isLoading

  useEffect(() => {
    if (authSettingsQuery.error) {
      toast.error(authSettingsQuery.error instanceof Error ? authSettingsQuery.error.message : messages.proxyApiKeysData.loadAuthStatusFailed)
    }
  }, [authSettingsQuery.error, messages.proxyApiKeysData.loadAuthStatusFailed])

  useEffect(() => {
    if (proxyKeysQuery.error) {
      toast.error(proxyKeysQuery.error instanceof Error ? proxyKeysQuery.error.message : messages.proxyApiKeysData.loadKeysFailed)
    }
  }, [messages.proxyApiKeysData.loadKeysFailed, proxyKeysQuery.error])

  const createMutation = useMutation({ mutationFn: api.settings.auth.proxyKeys.create })
  const rotateMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.rotate(keyId) })
  const updateMutation = useMutation({
    mutationFn: ({ keyId, payload }: { keyId: number; payload: ProxyApiKeyUpdate }) =>
      api.settings.auth.proxyKeys.update(keyId, payload),
  })
  const deleteMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.delete(keyId) })

  const createDisabled = createMutation.isPending || !authSettings || remainingKeys === 0

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
      reconcileLedgerFromMutation(created.item, created.capacity)
      toast.success(messages.proxyApiKeysData.created)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.createFailed)
    }
  }

  async function handleRotateProxyKey(keyId: number) {
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
      const rotationTimestamp = rotated.item.created_at
      reconcileLedgerFromMutation(rotated.item, rotated.capacity)
      // Mark the predecessor retired locally pending reconciliation.
      queryClient.setQueryData<ProxyApiKeyListData>(rewriteQueryKeys.global.proxyApiKeys(), (current) => {
        if (!current) {
          return current
        }
        return {
          items: current.items.map((key) =>
            key.id === keyId
              ? { ...key, is_active: false, expires_at: rotationTimestamp, updated_at: rotationTimestamp }
              : key,
          ),
          capacity: rotated.capacity,
        }
      })
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

  return {
    authSettings,
    authStatusLabel,
    authStatusTone,
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
    pageLoading,
    proxyKeyExpiresAt,
    proxyKeyLimit,
    proxyKeyName,
    proxyKeyNotes,
    proxyKeySuccessorByParentId: (() => {
      const map = new Map<number, number>()
      for (const key of proxyKeys) {
        if (key.rotated_from_id !== null) map.set(key.rotated_from_id, key.id)
      }
      return map
    })(),
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

