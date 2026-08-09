import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { getStaticMessages } from "@/i18n/staticMessages"
import { useTimezone } from "@/hooks/useTimezone"
import { api } from "@/lib/api"
import { extractEndpointFieldErrors, innerDetail, isEndpointConfigChangedError, isEndpointInUseError, isReferenceIntegrityError } from "@/lib/api/endpointErrors"
import { ApiError } from "@/lib/api/core"
import type { Endpoint, EndpointReferenceDetail, EndpointReferenceItem, EndpointReferencePage, EndpointReferenceSummary, EndpointVerifyResult } from "@/lib/types"
import { getSharedEndpoints, setSharedEndpoints } from "@/lib/referenceData"
import { extractServerValidation } from "@/shared/forms/serverValidation"
import { buildEndpointCreatePayload, buildEndpointUpdatePayload, type EndpointFormValues } from "./endpointSchemas"
import { useEndpointReferences, type EndpointReferenceSummaryState } from "./useEndpointReferences"
import type { OrphanCleanupEndpoint } from "@/pages/endpoints/OrphanCleanupDialog"

export type ReviewFilter = "all" | "referenced" | "unreferenced" | "inactive_only"

export type EndpointSortKey = "name" | "updated_at" | "direct_reference_count"

export type DeleteDialogState =
  | { phase: "closed" }
  | { phase: "checking"; endpoint: Endpoint; generation: number }
  | { phase: "eligible"; endpoint: Endpoint; summary: EndpointReferenceSummary; generation: number }
  | { phase: "blocked"; endpoint: Endpoint; detail: EndpointReferenceDetail; generation: number }
  | { phase: "check_error"; endpoint: Endpoint; error: ApiError; generation: number }
  | { phase: "integrity_error"; endpoint: Endpoint; error: ApiError; generation: number }
  | { phase: "deleting"; endpoint: Endpoint; generation: number }

export type VerifyDraftState =
  | { phase: "idle" }
  | { phase: "saving"; family?: string }
  | { phase: "saved"; family?: string }
  | { phase: "verifying"; family: string; revision: number }
  | { phase: "saved_and_verified"; family: string; result: EndpointVerifyResult }
  | { phase: "saved_and_verification_failed"; family: string; result: EndpointVerifyResult }
  | { phase: "saved_and_stale_result"; family: string; result: EndpointVerifyResult }

function referenceSummaryForState(summary: EndpointReferenceSummaryState | undefined): EndpointReferenceSummary | null {
  if (!summary) return null
  if (summary.status === "ready" || summary.status === "stale") return summary.value
  return null
}

export function useEndpointsFeatureData() {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [editingEndpoint, setEditingEndpointState] = useState<Endpoint | null>(null)
  const [endpointDialogError, setEndpointDialogError] = useState<string | null>(null)
  const [endpointFieldErrors, setEndpointFieldErrors] = useState<Record<string, string> | null>(null)
  const [duplicatingEndpointId, setDuplicatingEndpointId] = useState<number | null>(null)
  const [deleteDialog, setDeleteDialog] = useState<DeleteDialogState>({ phase: "closed" })
  const [orphanCleanupTarget, setOrphanCleanupTarget] = useState<{ endpoint: OrphanCleanupEndpoint; item: EndpointReferenceItem } | null>(null)
  const [searchQuery, setSearchQuery] = useState("")
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>("all")
  const [sortKey, setSortKey] = useState<EndpointSortKey>("name")
  const [sortDescending, setSortDescending] = useState(false)
  const [attachModelTarget, setAttachModelTarget] = useState<Endpoint | null>(null)
  const { format: formatTime } = useTimezone()

  const revision = 0

  const references = useEndpointReferences(endpoints.map((endpoint) => endpoint.id))

  // Initial load from the shared Endpoint cache owner.
  useEffect(() => {
    let cancelled = false
    setIsLoading(true)
    void (async () => {
      try {
        const loaded = await getSharedEndpoints(revision, true)
        if (cancelled) return
        setEndpoints(loaded)
      } catch {
        if (!cancelled) toast.error(getStaticMessages().endpointsData.loadFailed)
      } finally {
        if (!cancelled) setIsLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [revision])

  const commitEndpoints = useCallback((updater: (current: Endpoint[]) => Endpoint[]) => {
    setEndpoints((current) => {
      const next = updater(current)
      setSharedEndpoints(revision, next)
      return next
    })
  }, [revision])

  const normalizedSearch = searchQuery.trim().toLowerCase()

  const filterDisabled = references.hasUnknownOrStale

  // Reference filter normalizes to all when any item is not fresh-ready.
  const effectiveFilter: ReviewFilter = filterDisabled ? "all" : reviewFilter

  const filteredEndpoints = useMemo(() => {
    return endpoints.filter((endpoint) => {
      const matchesSearch =
        normalizedSearch.length === 0 ||
        endpoint.name.toLowerCase().includes(normalizedSearch) ||
        endpoint.base_url.toLowerCase().includes(normalizedSearch)
      if (!matchesSearch) return false
      if (effectiveFilter === "all") return true
      const summary = references.summaries[endpoint.id]
      const value = referenceSummaryForState(summary)
      if (!value) return false
      if (effectiveFilter === "referenced") return value.direct_reference_count > 0
      if (effectiveFilter === "unreferenced") return value.direct_reference_count === 0
      if (effectiveFilter === "inactive_only") {
        return value.direct_reference_count > 0 && value.enabled_reference_count === 0
      }
      return true
    })
  }, [effectiveFilter, endpoints, normalizedSearch, references.summaries])

  const sortedEndpoints = useMemo(() => {
    const items = [...filteredEndpoints]
    const direction = sortDescending ? -1 : 1
    items.sort((left, right) => {
      let comparison = 0
      if (sortKey === "name") {
        comparison = left.name.localeCompare(right.name, "zh-CN")
        if (comparison === 0) comparison = left.id - right.id
      } else if (sortKey === "updated_at") {
        comparison = left.updated_at.localeCompare(right.updated_at)
        if (comparison === 0) comparison = left.id - right.id
      } else if (sortKey === "direct_reference_count") {
        const leftSummary = referenceSummaryForState(references.summaries[left.id])
        const rightSummary = referenceSummaryForState(references.summaries[right.id])
        const leftCount = leftSummary?.direct_reference_count ?? 0
        const rightCount = rightSummary?.direct_reference_count ?? 0
        comparison = leftCount - rightCount
        if (comparison === 0) comparison = left.name.localeCompare(right.name, "zh-CN")
      }
      return comparison * direction
    })
    return items
  }, [filteredEndpoints, references.summaries, sortDescending, sortKey])

  const toggleSort = useCallback((key: EndpointSortKey) => {
    if (key === "direct_reference_count" && filterDisabled) return
    if (sortKey === key) {
      setSortDescending((current) => !current)
    } else {
      setSortKey(key)
      setSortDescending(false)
    }
  }, [filterDisabled, sortKey])

  const openCreateDialog = (open: boolean) => {
    if (open) {
      setEndpointDialogError(null)
      setEndpointFieldErrors(null)
    }
    setIsCreateOpen(open)
  }

  const setEditingEndpoint = (endpoint: Endpoint | null) => {
    if (endpoint) {
      setEndpointDialogError(null)
      setEndpointFieldErrors(null)
    }
    setEditingEndpointState(endpoint)
  }

  const handleCreate = async (values: EndpointFormValues, verifyFamily?: string) => {
    const messages = getStaticMessages()
    try {
      const created = await api.endpoints.create(buildEndpointCreatePayload(values))
      toast.success(messages.endpointsData.created)
      if (!verifyFamily) {
        setIsCreateOpen(false)
      }
      commitEndpoints((current) => [...current, created])
      references.addEndpoint(created.id)
      setAttachModelTarget(created)
      if (verifyFamily) {
        const verifyResult = await handleVerify(created.id, verifyFamily, created.config_revision)
        return { endpoint: created, verifyFamily, verifyResult }
      }
      return { endpoint: created, verifyFamily }
    } catch (error) {
      const fieldErrors = extractEndpointFieldErrors(error)
      if (fieldErrors) {
        setEndpointFieldErrors(fieldErrors)
      }
      const validation = extractServerValidation(error, messages.endpointsData.createFailed)
      setEndpointDialogError(validation.summary)
      toast.error(validation.summary)
      return null
    }
  }

  const handleUpdate = async (values: EndpointFormValues, verifyFamily?: string) => {
    const messages = getStaticMessages()
    if (!editingEndpoint) return null
    try {
      const updated = await api.endpoints.update(editingEndpoint.id, buildEndpointUpdatePayload(values))
      const keyRotated = updated.api_key_updated_at !== editingEndpoint.api_key_updated_at
      toast.success(keyRotated && updated.api_key_fingerprint ? messages.endpointsData.keyRotated(updated.api_key_fingerprint) : messages.endpointsData.keyUnchanged)
      if (!verifyFamily) {
        setEditingEndpoint(null)
      }
      commitEndpoints((current) => current.map((endpoint) => (endpoint.id === updated.id ? updated : endpoint)))
      references.invalidateEndpoint(updated.id)
      if (verifyFamily) {
        const verifyResult = await handleVerify(updated.id, verifyFamily, updated.config_revision)
        return { endpoint: updated, verifyFamily, verifyResult }
      }
      return { endpoint: updated, verifyFamily }
    } catch (error) {
      const fieldErrors = extractEndpointFieldErrors(error)
      if (fieldErrors) {
        setEndpointFieldErrors(fieldErrors)
      }
      const validation = extractServerValidation(error, messages.endpointsData.updateFailed)
      setEndpointDialogError(validation.summary)
      toast.error(validation.summary)
      return null
    }
  }

  const handleVerify = async (endpointId: number, family: string, expectedRevision: number): Promise<EndpointVerifyResult | null> => {
    const messages = getStaticMessages()
    try {
      const result = await api.endpoints.verify(endpointId, { api_family: family as never, expected_config_revision: expectedRevision })
      if (result.is_current && result.config_revision === expectedRevision) {
        return result
      }
      return result
    } catch (error) {
      if (isEndpointConfigChangedError(error)) {
        const changed = innerDetail<{ endpoint: Endpoint }>(error)
        if (changed?.endpoint) {
          commitEndpoints((current) => current.map((endpoint) => (endpoint.id === endpointId ? changed.endpoint : endpoint)))
        }
        return null
      }
      toast.error(messages.endpointsData.verifyFailed)
      return null
    }
  }

  const handleDeleteRequest = (endpoint: Endpoint) => {
    // Every dialog open runs a fresh single-reference preflight.
    const generation = Date.now()
    setDeleteDialog({ phase: "checking", endpoint, generation })
    void (async () => {
      try {
        const detail = await api.endpoints.referencesDetail(endpoint.id)
        setDeleteDialog((current) => {
          if (current.phase !== "checking" || current.endpoint.id !== endpoint.id) return current
          if (detail.summary.direct_reference_count === 0) {
            references.loadDetail(endpoint.id)
            return { phase: "eligible", endpoint, summary: detail.summary, generation }
          }
          return { phase: "blocked", endpoint, detail, generation }
        })
      } catch (error) {
        const apiError = error instanceof ApiError ? error : new ApiError(error instanceof Error ? error.message : "Failed to check references", 0, null)
        setDeleteDialog((current) => {
          if (current.phase !== "checking" || current.endpoint.id !== endpoint.id) return current
          if (isReferenceIntegrityError(error)) {
            return { phase: "integrity_error", endpoint, error: apiError, generation }
          }
          return { phase: "check_error", endpoint, error: apiError, generation }
        })
      }
    })()
  }

  const handleDeleteConfirm = async (target: { id: number }) => {
    const current = deleteDialog
    const endpoint = current.phase !== "closed" && current.endpoint.id === target.id
      ? current.endpoint
      : endpoints.find((item) => item.id === target.id)
    if (!endpoint) return
    const messages = getStaticMessages()
    setDeleteDialog((currentState) => ({ ...currentState, phase: "deleting" } as DeleteDialogState))
    try {
      await api.endpoints.delete(endpoint.id)
      toast.success(messages.endpointsData.deleted)
      setDeleteDialog({ phase: "closed" })
      references.removeEndpoint(endpoint.id)
      commitEndpoints((current) => current.filter((item) => item.id !== endpoint.id))
    } catch (error) {
      if (isEndpointInUseError(error)) {
        // Race: a reference appeared after preflight. Replace the dialog with
        // the response's latest summary + bounded first page.
        const race = innerDetail<{ endpoint_id: number; summary: EndpointReferenceSummary; reference_page: EndpointReferencePage }>(error)
        if (!race) return
        const detail: EndpointReferenceDetail = {
          endpoint_id: race.endpoint_id,
          summary: race.summary,
          reference_page: race.reference_page,
        }
        setDeleteDialog((current) => {
          if (current.phase !== "deleting" || current.endpoint.id !== endpoint.id) return current
          return { phase: "blocked", endpoint, detail, generation: Date.now() }
        })
        return
      }
      if (isReferenceIntegrityError(error)) {
        setDeleteDialog((current) => {
          if (current.phase !== "deleting" || current.endpoint.id !== endpoint.id) return current
          return { phase: "integrity_error", endpoint, error: error instanceof ApiError ? error : new ApiError("Integrity error", 409, null), generation: Date.now() }
        })
        return
      }
      setDeleteDialog((current) => {
        if (current.phase !== "deleting" || current.endpoint.id !== endpoint.id) return current
        return { phase: "check_error", endpoint, error: error instanceof ApiError ? error : new ApiError(error instanceof Error ? error.message : messages.endpointsData.deleteFailed, 0, null), generation: Date.now() }
      })
    }
  }

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (!open && deleteDialog.phase !== "deleting") {
      setDeleteDialog({ phase: "closed" })
    }
  }

  const handleDeleteRetry = () => {
    if (deleteDialog.phase === "closed") return
    const endpoint = deleteDialog.endpoint
    setDeleteDialog({ phase: "closed" })
    handleDeleteRequest(endpoint)
  }

  const handleLoadMoreBlockers = async (endpointId: number) => {
    await references.loadMore(endpointId)
  }

  const handleOrphanCleanup = async (endpoint: OrphanCleanupEndpoint, item: EndpointReferenceItem) => {
    const messages = getStaticMessages()
    try {
      await api.endpoints.orphanCleanup(endpoint.id, item.connection_id)
      toast.success(messages.endpointsData.orphanCleaned)
      setOrphanCleanupTarget(null)
      references.invalidateEndpoint(endpoint.id)
    } catch (error) {
      if (isReferenceIntegrityError(error)) {
        toast.error(messages.endpointsData.orphanCleanupFailed)
        references.invalidateEndpoint(endpoint.id)
        return
      }
      toast.error(error instanceof Error ? error.message : messages.endpointsData.orphanCleanupFailed)
    }
  }

  const handleDuplicateEndpoint = async (endpoint: Endpoint) => {
    const messages = getStaticMessages()
    setDuplicatingEndpointId(endpoint.id)
    try {
      const duplicate = await api.endpoints.duplicate(endpoint.id)
      toast.success(messages.endpointsData.duplicatedAs(duplicate.name))
      commitEndpoints((current) => [...current, duplicate])
      references.addEndpoint(duplicate.id)
      setAttachModelTarget(duplicate)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.duplicateFailed)
    } finally {
      setDuplicatingEndpointId(null)
    }
  }

  const handleAttachNavigate = (endpoint: Endpoint) => {
    // One-shot attach: open the model picker; the selected model detail page
    // consumes action=create-terminal-target + endpoint_id (never key material).
    setAttachModelTarget(endpoint)
  }

  const handleAttachModelSelected = (modelId: number) => {
    if (!attachModelTarget) return
    const endpoint = attachModelTarget
    setAttachModelTarget(null)
    window.location.assign(`/models/${modelId}?action=create-terminal-target&endpoint_id=${endpoint.id}`)
  }

  return {
    attachModelTarget,
    deleteDialog,
    duplicatingEndpointId,
    editingEndpoint,
    effectiveFilter,
    endpointDialogError,
    endpointFieldErrors,
    endpoints,
    filterDisabled,
    filteredEndpoints: sortedEndpoints,
    formatTime,
    handleAttachModelSelected,
    handleAttachNavigate,
    handleCreate,
    handleDeleteConfirm,
    handleDeleteDialogOpenChange,
    handleDeleteRequest,
    handleDeleteRetry,
    handleDuplicateEndpoint,
    handleLoadMoreBlockers,
    handleOrphanCleanup,
    handleUpdate,
    handleVerify,
    isCreateOpen,
    isLoading,
    orphanCleanupTarget,
    references,
    reviewFilter: effectiveFilter,
    searchQuery,
    setAttachModelTarget,
    setDeleteDialog,
    setEditingEndpoint,
    setIsCreateOpen: openCreateDialog,
    setOrphanCleanupTarget,
    setReviewFilter,
    setSearchQuery,
    setSortDescending,
    setSortKey,
    sortDescending,
    sortKey,
    toggleSort,
  }
}
