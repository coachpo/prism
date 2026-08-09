import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { getStaticMessages } from "@/i18n/staticMessages"
import { useTimezone } from "@/hooks/useTimezone"
import type { Endpoint } from "@/lib/types"
import type { EndpointReferenceView } from "@/pages/endpoints/EndpointCard"
import type { OpenAITextCapability } from "@/lib/types"
import { extractServerValidation } from "@/shared/forms/serverValidation"
import { buildEndpointCreatePayload, buildEndpointUpdatePayload, hasEndpointReviewFilters, type EndpointFormValues } from "./endpointSchemas"
import { useEndpointBootstrapData } from "@/pages/endpoints/useEndpointBootstrapData"
import { useEndpointReorder } from "@/pages/endpoints/useEndpointReorder"

export type ReviewFilter = "all" | "in-use" | "unused"

export function useEndpointsFeatureData() {
  const [isDeletingEndpoint, setIsDeletingEndpoint] = useState(false)
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [editingEndpoint, setEditingEndpointState] = useState<Endpoint | null>(null)
  const [endpointDialogError, setEndpointDialogError] = useState<string | null>(null)
  const [duplicatingEndpointId, setDuplicatingEndpointId] = useState<number | null>(null)
  const [deleteTarget, setDeleteTargetState] = useState<Endpoint | null>(null)
  const [deleteDialogTarget, setDeleteDialogTarget] = useState<Endpoint | null>(null)
  const [searchQuery, setSearchQuery] = useState("")
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>("all")
  const [directReferencesByEndpoint, setDirectReferencesByEndpoint] = useState<Record<number, EndpointReferenceView[]>>({})
  const [attachTarget, setAttachTarget] = useState<Endpoint | null>(null)
  const revision = 0
  const { format: formatTime } = useTimezone()
  const { commitEndpoints, endpointModels, endpoints, isLoading, setEndpoints } = useEndpointBootstrapData(revision)
  const normalizedSearch = searchQuery.trim().toLowerCase()
  const hasActiveReviewFilters = hasEndpointReviewFilters({ searchQuery, reviewFilter })

  const filteredEndpoints = useMemo(() => endpoints.filter((endpoint) => {
    const models = endpointModels[endpoint.id] ?? []
    const matchesSearch = normalizedSearch.length === 0 || endpoint.name.toLowerCase().includes(normalizedSearch) || endpoint.base_url.toLowerCase().includes(normalizedSearch)
    const matchesUsage = reviewFilter === "all" || (reviewFilter === "in-use" ? models.length > 0 : models.length === 0)
    return matchesSearch && matchesUsage
  }), [endpointModels, endpoints, normalizedSearch, reviewFilter])
  const reorder = useEndpointReorder({ endpoints, revision, setEndpoints, filtersActive: hasActiveReviewFilters })

  const setDeleteTarget = (target: Endpoint | null) => {
    if (target) setDeleteDialogTarget(target)
    setDeleteTargetState(target)
  }

  const openCreateDialog = (open: boolean) => {
    if (open) setEndpointDialogError(null)
    setIsCreateOpen(open)
  }

  const setEditingEndpoint = (endpoint: Endpoint | null) => {
    if (endpoint) setEndpointDialogError(null)
    setEditingEndpointState(endpoint)
  }

  const handleCreate = async (values: EndpointFormValues) => {
    const messages = getStaticMessages()
    try {
      const created = await api.endpoints.create(buildEndpointCreatePayload(values))
      toast.success(messages.endpointsData.created)
      setIsCreateOpen(false)
      commitEndpoints((current) => [...current, created].sort((left, right) => left.position - right.position), (current) => ({ ...current, [created.id]: [] }))
    } catch (error) {
      const validation = extractServerValidation(error, messages.endpointsData.createFailed)
      setEndpointDialogError(validation.summary)
      toast.error(validation.summary)
    }
  }

  const handleUpdate = async (values: EndpointFormValues) => {
    const messages = getStaticMessages()
    if (!editingEndpoint) return
    try {
      const updated = await api.endpoints.update(editingEndpoint.id, buildEndpointUpdatePayload(values))
      toast.success(messages.endpointsData.updated)
      setEditingEndpoint(null)
      commitEndpoints((current) => current.map((endpoint) => (endpoint.id === updated.id ? updated : endpoint)))
    } catch (error) {
      const validation = extractServerValidation(error, messages.endpointsData.updateFailed)
      setEndpointDialogError(validation.summary)
      toast.error(validation.summary)
    }
  }

  const handleDelete = async (id: number) => {
    const messages = getStaticMessages()
    setIsDeletingEndpoint(true)
    try {
      await api.endpoints.delete(id)
      toast.success(messages.endpointsData.deleted)
      setDeleteTarget(null)
      commitEndpoints((current) => current.filter((endpoint) => endpoint.id !== id), (current) => {
        const next = { ...current }
        delete next[id]
        return next
      })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.deleteFailed)
    } finally {
      setIsDeletingEndpoint(false)
    }
  }

  const handleDuplicateEndpoint = async (endpoint: Endpoint) => {
    const messages = getStaticMessages()
    setDuplicatingEndpointId(endpoint.id)
    try {
      const duplicate = await api.endpoints.duplicate(endpoint.id)
      toast.success(messages.endpointsData.duplicatedAs(duplicate.name))
      commitEndpoints((current) => [...current, duplicate].sort((left, right) => left.position - right.position), (current) => ({ ...current, [duplicate.id]: [] }))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.duplicateFailed)
    } finally {
      setDuplicatingEndpointId(null)
    }
  }

  const refreshDirectReferences = useCallback(async () => {
    if (endpoints.length === 0) return
    try {
      const response = await api.endpoints.referencesBatch(endpoints.map((endpoint) => endpoint.id))
      const byEndpoint: Record<number, EndpointReferenceView[]> = {}
      for (const item of response.items) {
        byEndpoint[item.endpoint_id] = item.references.map((reference) => ({
          connection_id: reference.connection_id,
          terminal_target_name: reference.terminal_target_name,
          model_config_id: reference.model_config_id,
          model_id: reference.model_id,
          model_display_name: reference.model_display_name,
          api_family: reference.api_family,
          is_enabled: reference.is_enabled,
          is_active: reference.is_active,
          openai_text_capability: reference.openai_text_capability,
          pricing_template: reference.pricing_template,
        }))
      }
      setDirectReferencesByEndpoint(byEndpoint)
    } catch (error) {
      console.error("Failed to load endpoint direct references", error)
    }
  }, [endpoints])

  useEffect(() => {
    void refreshDirectReferences()
  }, [refreshDirectReferences])

  const handleAttachEndpoint = async (endpoint: Endpoint, destinationModelConfigId: number, capability: string | null, targetName: string) => {
    try {
      await api.models.connections.create(destinationModelConfigId, {
        api_family: "openai",
        endpoint_id: endpoint.id,
        name: targetName.trim() || undefined,
        is_active: true,
        openai_text_capability: capability as OpenAITextCapability | undefined,
      })
      toast.success(getStaticMessages().modelDetailData.connectionCreated)
      setAttachTarget(null)
      void refreshDirectReferences()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to attach endpoint")
      throw error
    }
  }

  return { attachTarget, deleteTarget, deleteDialogTarget, directReferencesByEndpoint, duplicatingEndpointId, editingEndpoint, endpointDialogError, endpointModels, endpoints, filteredEndpoints, formatTime, hasActiveReviewFilters, handleAttachEndpoint, handleCreate, handleDelete, handleDeleteDialogOpenChange: (open: boolean) => !open && !isDeletingEndpoint && setDeleteTarget(null), handleDuplicateEndpoint, handleUpdate, isCreateOpen, isDeletingEndpoint, isLoading, reviewFilter, searchQuery, setAttachTarget, setDeleteTarget, setEditingEndpoint, setIsCreateOpen: openCreateDialog, setReviewFilter, setSearchQuery, ...reorder }
}
