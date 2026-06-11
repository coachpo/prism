import type { ComponentProps } from "react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { getStaticMessages } from "@/i18n/staticMessages"
import { api } from "@/lib/api"
import type { ProxyApiKey, ProxyApiKeyUpdate } from "@/lib/types"
import { rewriteQueryKeys } from "@/shared/api/queryKeys"
import {
  getAuthStatusTone,
  normalizeExpiresAtInput,
  toDateTimeLocalValue,
} from "@/pages/proxy-api-keys/proxyKeyFormatting"

type FormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0]

type GeneratedSecret = {
  keyId: number
  value: string
}

export function useProxyKeysFeatureData() {
  const queryClient = useQueryClient()
  const messages = getStaticMessages()
  const [proxyKeyName, setProxyKeyName] = useState("")
  const [proxyKeyNotes, setProxyKeyNotes] = useState("")
  const [proxyKeyExpiresAt, setProxyKeyExpiresAt] = useState("")
  const [deleteConfirm, setDeleteConfirm] = useState<ProxyApiKey | null>(null)
  const [deleteProxyKeyAlertOpen, setDeleteProxyKeyAlertOpen] = useState(false)
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] = useState<ProxyApiKey | null>(null)
  const [editingProxyKey, setEditingProxyKey] = useState<ProxyApiKey | null>(null)
  const [editProxyKeySheetOpen, setEditProxyKeySheetOpen] = useState(false)
  const [editingProxyKeyName, setEditingProxyKeyName] = useState("")
  const [editingProxyKeyNotes, setEditingProxyKeyNotes] = useState("")
  const [editingProxyKeyExpiresAt, setEditingProxyKeyExpiresAt] = useState("")
  const [editingProxyKeyActive, setEditingProxyKeyActive] = useState(false)
  const [latestGeneratedKeyState, setLatestGeneratedKeyState] = useState<GeneratedSecret | null>(null)

  const authSettingsQuery = useQuery({
    queryKey: rewriteQueryKeys.global.settingsAuth(),
    queryFn: api.settings.auth.get,
  })
  const proxyKeysQuery = useQuery({
    queryKey: rewriteQueryKeys.global.proxyApiKeys(),
    queryFn: api.settings.auth.proxyKeys.list,
  })

  const authSettings = authSettingsQuery.data ?? null
  const proxyKeys = useMemo(() => proxyKeysQuery.data ?? [], [proxyKeysQuery.data])
  const latestGeneratedKey = latestGeneratedKeyState && proxyKeys.some((key) => key.id === latestGeneratedKeyState.keyId)
    ? latestGeneratedKeyState.value
    : null
  const clearLatestGeneratedKey = useCallback(() => setLatestGeneratedKeyState(null), [])

  const setProxyKeys = useCallback(
    (updater: (current: ProxyApiKey[]) => ProxyApiKey[]) => {
      queryClient.setQueryData<ProxyApiKey[]>(rewriteQueryKeys.global.proxyApiKeys(), (current) => updater(current ?? []))
    },
    [queryClient],
  )

  const displayedProxyKeys = useMemo(
    () => [...proxyKeys].sort((left, right) => right.id - left.id),
    [proxyKeys],
  )
  const proxyKeySuccessorByParentId = useMemo(() => {
    const map = new Map<number, number>()
    for (const key of proxyKeys) {
      if (key.rotated_from_id !== null) map.set(key.rotated_from_id, key.id)
    }
    return map
  }, [proxyKeys])

  const proxyKeyLimit = authSettings?.proxy_key_limit ?? 100
  const remainingKeys = authSettings ? Math.max(proxyKeyLimit - proxyKeys.length, 0) : 0
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

  useEffect(() => clearLatestGeneratedKey, [clearLatestGeneratedKey])

  const createMutation = useMutation({ mutationFn: api.settings.auth.proxyKeys.create })
  const rotateMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.rotate(keyId) })
  const updateMutation = useMutation({
    mutationFn: ({ keyId, payload }: { keyId: number; payload: ProxyApiKeyUpdate }) =>
      api.settings.auth.proxyKeys.update(keyId, payload),
  })
  const deleteMutation = useMutation({ mutationFn: (keyId: number) => api.settings.auth.proxyKeys.delete(keyId) })

  const createDisabled = createMutation.isPending || !authSettings || remainingKeys === 0

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
        expires_at: normalizeExpiresAtInput(proxyKeyExpiresAt),
      })
      setLatestGeneratedKeyState({ keyId: created.item.id, value: created.key })
      setProxyKeyName("")
      setProxyKeyNotes("")
      setProxyKeyExpiresAt("")
      setProxyKeys((current) => [created.item, ...current])
      toast.success(messages.proxyApiKeysData.created)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.createFailed)
    }
  }

  async function handleRotateProxyKey(keyId: number) {
    try {
      const rotated = await rotateMutation.mutateAsync(keyId)
      setLatestGeneratedKeyState({ keyId: rotated.item.id, value: rotated.key })
      setProxyKeys((current) => {
        const rotationTimestamp = rotated.item.created_at
        const withoutSuccessorDuplicates = current.filter((key) => key.id !== rotated.item.id)
        return [
          rotated.item,
          ...withoutSuccessorDuplicates.map((key) =>
            key.id === keyId
              ? {
                  ...key,
                  is_active: false,
                  expires_at: rotationTimestamp,
                  updated_at: rotationTimestamp,
                }
              : key,
          ),
        ]
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
      await deleteMutation.mutateAsync(deletingKey.id)
      setProxyKeys((current) => current.filter((key) => key.id !== deletingKey.id))
      if (latestGeneratedKeyState?.keyId === deletingKey.id) setLatestGeneratedKeyState(null)
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
    setEditingProxyKeyExpiresAt(toDateTimeLocalValue(item.expires_at))
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
        expires_at: normalizeExpiresAtInput(editingProxyKeyExpiresAt),
      }
      const updated = await updateMutation.mutateAsync({ keyId: editingProxyKey.id, payload })
      setProxyKeys((current) => current.map((key) => (key.id === updated.id ? updated : key)))
      setEditingProxyKey(updated)
      setEditingProxyKeyName(updated.name)
      setEditingProxyKeyNotes(updated.notes ?? "")
      setEditingProxyKeyExpiresAt(toDateTimeLocalValue(updated.expires_at))
      setEditingProxyKeyActive(updated.is_active)
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
    clearLatestGeneratedKey,
    authStatusTone,
    createDisabled,
    creatingProxyKey: createMutation.isPending,
    deleteConfirm,
    deleteProxyKeyAlertOpen,
    deletingProxyKeyId: deleteMutation.isPending ? deleteMutation.variables ?? null : null,
    displayedDeleteConfirm,
    editProxyKeySheetOpen,
    editingProxyKey,
    editingProxyKeyActive,
    editingProxyKeyExpiresAt,
    editingProxyKeyName,
    editingProxyKeyNotes,
    displayedProxyKeys,
    handleCreateSubmit,
    handleDeleteDialogOpenChange,
    handleDeleteProxyKey,
    handleEditDialogOpenChange,
    handleEditSubmit,
    handleRotateProxyKey,
    latestGeneratedKey,
    pageLoading,
    proxyKeyExpiresAt,
    proxyKeyLimit,
    proxyKeyName,
    proxyKeyNotes,
    proxyKeySuccessorByParentId,
    proxyKeys,
    remainingKeys,
    rotatingProxyKeyId: rotateMutation.isPending ? rotateMutation.variables ?? null : null,
    savingEditedProxyKeyId: updateMutation.isPending ? updateMutation.variables?.keyId ?? null : null,
    setDeleteConfirm: setDeleteConfirmState,
    setEditingProxyKeyActive,
    setEditingProxyKeyExpiresAt,
    setEditingProxyKeyName,
    setEditingProxyKeyNotes,
    setLatestGeneratedKeyState,
    setProxyKeyExpiresAt,
    setProxyKeyName,
    setProxyKeyNotes,
    startEditingProxyKey,
  }
}
