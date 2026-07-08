import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { api, ApiError } from "@/lib/api"
import { getStaticMessages } from "@/i18n/staticMessages"
import { getSharedPricingTemplates, setSharedPricingTemplates } from "@/lib/referenceData"
import type { PricingTemplate, PricingTemplateConnectionUsageItem, PricingTemplateImportRequest } from "@/lib/types"
import { extractServerValidation } from "@/shared/forms/serverValidation"
import { buildPricingTemplateCreatePayload, buildPricingTemplateUpdatePayload, parsePricingTemplateUsageRows, type PricingTemplateFormValues } from "./pricingSchemas"

export function usePricingFeatureData(revision: number) {
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([])
  const [pricingTemplatesLoading, setPricingTemplatesLoading] = useState(false)
  const [pricingTemplateDialogOpen, setPricingTemplateDialogOpen] = useState(false)
  const [editingPricingTemplate, setEditingPricingTemplate] = useState<PricingTemplate | null>(null)
  const [pricingTemplatePreparingEditId, setPricingTemplatePreparingEditId] = useState<number | null>(null)
  const [pricingTemplateSaving, setPricingTemplateSaving] = useState(false)
  const [pricingTemplateServerError, setPricingTemplateServerError] = useState<string | null>(null)
  const [pricingTemplateUsageDialogOpen, setPricingTemplateUsageDialogOpen] = useState(false)
  const [pricingTemplateUsageRows, setPricingTemplateUsageRows] = useState<PricingTemplateConnectionUsageItem[]>([])
  const [pricingTemplateUsageLoading, setPricingTemplateUsageLoading] = useState(false)
  const [pricingTemplateUsageTemplate, setPricingTemplateUsageTemplate] = useState<PricingTemplate | null>(null)
  const [deletePricingTemplateConfirm, setDeletePricingTemplateConfirmState] = useState<PricingTemplate | null>(null)
  const [deletePricingTemplateDisplay, setDeletePricingTemplateDisplay] = useState<PricingTemplate | null>(null)
  const [deletePricingTemplateConflict, setDeletePricingTemplateConflict] = useState<PricingTemplateConnectionUsageItem[] | null>(null)
  const [pricingTemplateUsageError, setPricingTemplateUsageError] = useState(false)
  const [pricingTemplateDeleting, setPricingTemplateDeleting] = useState(false)
  const [pricingTemplateImportDialogOpen, setPricingTemplateImportDialogOpen] = useState(false)
  const [pricingTemplateImporting, setPricingTemplateImporting] = useState(false)

  const setDeletePricingTemplateConfirm = (template: PricingTemplate | null) => {
    if (template) setDeletePricingTemplateDisplay(template)
    setDeletePricingTemplateConfirmState(template)
  }
  const commitPricingTemplates = useCallback((updater: (current: PricingTemplate[]) => PricingTemplate[]) => {
    setPricingTemplates((current) => {
      const next = sortPricingTemplates(updater(current))
      setSharedPricingTemplates(revision, next)
      return next
    })
  }, [revision])
  const fetchPricingTemplates = useCallback(async () => {
    const messages = getStaticMessages()
    setPricingTemplatesLoading(true)
    try {
      setPricingTemplates(sortPricingTemplates(await getSharedPricingTemplates(revision)))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadFailed)
    } finally {
      setPricingTemplatesLoading(false)
    }
  }, [revision])
  useEffect(() => { void fetchPricingTemplates() }, [fetchPricingTemplates])

  const closePricingTemplateDialog = () => {
    setPricingTemplateDialogOpen(false)
    setPricingTemplateServerError(null)
  }
  const openCreatePricingTemplateDialog = () => {
    setEditingPricingTemplate(null)
    setPricingTemplatePreparingEditId(null)
    setPricingTemplateServerError(null)
    setPricingTemplateDialogOpen(true)
  }
  const handleEditPricingTemplate = async (templateSummary: PricingTemplate) => {
    const messages = getStaticMessages()
    setPricingTemplatePreparingEditId(templateSummary.id)
    try {
      setEditingPricingTemplate(await api.pricingTemplates.get(templateSummary.id))
      setPricingTemplateServerError(null)
      setPricingTemplateDialogOpen(true)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadSingleFailed)
    } finally {
      setPricingTemplatePreparingEditId(null)
    }
  }
  const handleSavePricingTemplate = async (values: PricingTemplateFormValues) => {
    const messages = getStaticMessages()
    setPricingTemplateServerError(null)
    setPricingTemplateSaving(true)
    try {
      if (editingPricingTemplate) {
        const updated = await api.pricingTemplates.update(editingPricingTemplate.id, buildPricingTemplateUpdatePayload(editingPricingTemplate, values))
        commitPricingTemplates((current) => current.map((template) => template.id === editingPricingTemplate.id ? updated : template))
        toast.success(messages.pricingTemplatesData.updated)
      } else {
        const created = await api.pricingTemplates.create(buildPricingTemplateCreatePayload(values))
        commitPricingTemplates((current) => [created, ...current])
        toast.success(messages.pricingTemplatesData.created)
      }
      closePricingTemplateDialog()
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setPricingTemplateServerError(messages.pricingTemplatesData.changedWhileEditing)
        toast.error(messages.pricingTemplatesData.changedWhileEditing)
        await fetchPricingTemplates()
        return
      }
      const validation = extractServerValidation(error, messages.pricingTemplatesData.saveFailed)
      setPricingTemplateServerError(validation.summary)
      toast.error(validation.summary)
    } finally {
      setPricingTemplateSaving(false)
    }
  }
  const handleViewPricingTemplateUsage = async (template: PricingTemplate) => {
    const messages = getStaticMessages()
    setPricingTemplateUsageTemplate(template)
    setPricingTemplateUsageDialogOpen(true)
    setPricingTemplateUsageLoading(true)
    try {
      const data = await api.pricingTemplates.connections(template.id)
      setPricingTemplateUsageRows(data.items)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadUsageFailed)
      setPricingTemplateUsageRows([])
    } finally {
      setPricingTemplateUsageLoading(false)
    }
  }
  const handleDeletePricingTemplateClick = async (template: PricingTemplate) => {
    const messages = getStaticMessages()
    setDeletePricingTemplateConfirm(template)
    setDeletePricingTemplateConflict(null)
    setPricingTemplateUsageError(false)
    setPricingTemplateUsageLoading(true)
    try {
      const data = await api.pricingTemplates.connections(template.id)
      setPricingTemplateUsageRows(data.items)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.loadUsageFailed)
      setPricingTemplateUsageError(true)
      setPricingTemplateUsageRows([])
    } finally {
      setPricingTemplateUsageLoading(false)
    }
  }
  const handleDeletePricingTemplate = async () => {
    const messages = getStaticMessages()
    if (!deletePricingTemplateConfirm) return
    setPricingTemplateDeleting(true)
    try {
      if (pricingTemplateUsageError) {
        toast.error(messages.pricingTemplatesData.loadUsageFailed)
        return
      }
      await api.pricingTemplates.delete(deletePricingTemplateConfirm.id)
      commitPricingTemplates((current) => current.filter((template) => template.id !== deletePricingTemplateConfirm.id))
      toast.success(messages.pricingTemplatesData.deleted)
      setDeletePricingTemplateConfirm(null)
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setDeletePricingTemplateConflict(parsePricingTemplateUsageRows(error.detail))
        toast.error(messages.pricingTemplatesData.inUseCannotDelete)
      } else {
        toast.error(error instanceof Error ? error.message : messages.pricingTemplatesData.deleteFailed)
      }
    } finally {
      setPricingTemplateDeleting(false)
    }
  }
  const handleImportPricingTemplates = async (request: PricingTemplateImportRequest) => {
    const messages = getStaticMessages()
    setPricingTemplateImporting(true)
    try {
      const result = await api.pricingTemplates.importTemplates(request)
      await fetchPricingTemplates()
      toast.success(messages.pricing.importResultSummary(result.created, result.updated, result.skipped.length))
      setPricingTemplateImportDialogOpen(false)
      return true
    } catch (error) {
      const body = error instanceof ApiError && error.detail && typeof error.detail === "object" ? error.detail as { errors?: Array<{ detail?: string }> } : null
      toast.error(body?.errors?.[0]?.detail || (error instanceof Error ? error.message : messages.common.requestFailed))
      return false
    } finally {
      setPricingTemplateImporting(false)
    }
  }
  return { closePricingTemplateDialog, deletePricingTemplateConfirm, deletePricingTemplateDisplay, deletePricingTemplateConflict, editingPricingTemplate, handleDeletePricingTemplate, handleDeletePricingTemplateClick, handleEditPricingTemplate, handleImportPricingTemplates, handleSavePricingTemplate, handleViewPricingTemplateUsage, openCreatePricingTemplateDialog, pricingTemplateDeleting, pricingTemplateDialogOpen, pricingTemplateImportDialogOpen, pricingTemplateImporting, pricingTemplatePreparingEditId, pricingTemplateSaving, pricingTemplateServerError, pricingTemplateUsageError, pricingTemplateUsageDialogOpen, pricingTemplateUsageLoading, pricingTemplateUsageRows, pricingTemplateUsageTemplate, pricingTemplates, pricingTemplatesLoading, setDeletePricingTemplateConfirm, setDeletePricingTemplateConflict, setPricingTemplateDialogOpen, setPricingTemplateImportDialogOpen, setPricingTemplateUsageDialogOpen }
}

function sortPricingTemplates(templates: PricingTemplate[]) {
  return [...templates].sort((left, right) => new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime() || right.id - left.id)
}
