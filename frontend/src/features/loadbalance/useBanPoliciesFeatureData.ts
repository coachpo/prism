import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { api, ApiError } from "@/lib/api"
import { getSharedLoadbalanceStrategies, setSharedLoadbalanceStrategies } from "@/lib/referenceData"
import type { LoadbalanceStrategy } from "@/lib/types"
import { banPolicyFormValuesFromStrategy, buildBanPolicyPayload, buildBanPolicyUpdatePayload, DEFAULT_BAN_POLICY_FORM_VALUES, getAttachedModelCountFromDeleteDetail, type BanPolicyFormValues } from "./banPolicySchemas"

export function useBanPoliciesFeatureData(revision: number) {
  const [strategies, setStrategies] = useState<LoadbalanceStrategy[]>([])
  const [strategiesLoading, setStrategiesLoading] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingStrategy, setEditingStrategy] = useState<LoadbalanceStrategy | null>(null)
  const [formValues, setFormValues] = useState<BanPolicyFormValues>(DEFAULT_BAN_POLICY_FORM_VALUES)
  const [saving, setSaving] = useState(false)
  const [defaultsCreating, setDefaultsCreating] = useState(false)
  const [preparingEditId, setPreparingEditId] = useState<number | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<LoadbalanceStrategy | null>(null)
  const [displayDelete, setDisplayDelete] = useState<LoadbalanceStrategy | null>(null)
  const [deleting, setDeleting] = useState(false)

  const commitStrategies = useCallback((updater: (current: LoadbalanceStrategy[]) => LoadbalanceStrategy[]) => {
    setStrategies((current) => {
      const next = sortStrategies(updater(current))
      setSharedLoadbalanceStrategies(revision, next)
      return next
    })
  }, [revision])

  const refreshStrategies = useCallback(async () => {
    setStrategiesLoading(true)
    try {
      const next = await getSharedLoadbalanceStrategies(revision)
      setStrategies(sortStrategies(next))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to load Ban Policy strategies")
    } finally {
      setStrategiesLoading(false)
    }
  }, [revision])

  useEffect(() => { void refreshStrategies() }, [refreshStrategies])

  const openCreate = () => {
    setEditingStrategy(null)
    setFormValues(DEFAULT_BAN_POLICY_FORM_VALUES)
    setDialogOpen(true)
  }

  const openEdit = async (strategy: LoadbalanceStrategy) => {
    setPreparingEditId(strategy.id)
    try {
      const loaded = await api.loadbalanceStrategies.get(strategy.id)
      setEditingStrategy(loaded)
      setFormValues(banPolicyFormValuesFromStrategy(loaded))
      setDialogOpen(true)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to load Ban Policy strategy")
    } finally {
      setPreparingEditId(null)
    }
  }

  const save = async (values: BanPolicyFormValues) => {
    setSaving(true)
    try {
      if (editingStrategy) {
        const updated = await api.loadbalanceStrategies.update(editingStrategy.id, buildBanPolicyUpdatePayload(values))
        commitStrategies((current) => current.map((strategy) => strategy.id === editingStrategy.id ? updated : strategy))
        toast.success("Ban Policy strategy updated")
      } else {
        const created = await api.loadbalanceStrategies.create(buildBanPolicyPayload(values))
        commitStrategies((current) => [created, ...current])
        toast.success("Ban Policy strategy created")
      }
      setDialogOpen(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to save Ban Policy strategy")
    } finally {
      setSaving(false)
    }
  }

  const createDefaults = async () => {
    setDefaultsCreating(true)
    try {
      const response = await api.loadbalanceStrategies.createDefaults()
      const next = sortStrategies(response.items)
      setStrategies(next)
      setSharedLoadbalanceStrategies(revision, next)
      toast.success(response.created_count > 0 ? "Default Ban Policy strategies created" : "Default Ban Policy strategies already exist")
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to create default Ban Policy strategies")
    } finally {
      setDefaultsCreating(false)
    }
  }

  const openDelete = (strategy: LoadbalanceStrategy) => {
    setDeleteConfirm(strategy)
    setDisplayDelete(strategy)
  }

  const closeDelete = () => {
    setDeleteConfirm(null)
    setDisplayDelete(null)
  }

  const deleteStrategy = async () => {
    if (!deleteConfirm) return
    setDeleting(true)
    try {
      await api.loadbalanceStrategies.delete(deleteConfirm.id)
      commitStrategies((current) => current.filter((strategy) => strategy.id !== deleteConfirm.id))
      toast.success("Ban Policy strategy deleted")
      closeDelete()
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const attachedModelCount = getAttachedModelCountFromDeleteDetail(error.detail)
        if (attachedModelCount !== null) {
          const blocked = { ...deleteConfirm, attached_model_count: attachedModelCount }
          setDeleteConfirm(blocked)
          setDisplayDelete(blocked)
        }
      }
      toast.error(error instanceof Error ? error.message : "Failed to delete Ban Policy strategy")
    } finally {
      setDeleting(false)
    }
  }

  return { strategies, strategiesLoading, dialogOpen, editingStrategy, formValues, saving, defaultsCreating, preparingEditId, deleteConfirm, displayDelete, deleting, refreshStrategies, openCreate, openEdit, save, createDefaults, openDelete, closeDelete, deleteStrategy, setDialogOpen }
}

function sortStrategies(strategies: LoadbalanceStrategy[]) {
  return [...strategies].sort((left, right) => {
    const updatedAtDelta = new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime()
    return updatedAtDelta !== 0 ? updatedAtDelta : right.id - left.id
  })
}
