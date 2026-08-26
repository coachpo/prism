import { useLoadbalanceStrategyDefaults } from "./useLoadbalanceStrategyDefaults";
import { useModelDeletion } from "./useModelDeletion";
import { useModelDialogMutations } from "./useModelDialogMutations";
import { useModelEnablementMutations } from "./useModelEnablementMutations";
import { useModelMetrics24h } from "./useModelMetrics24h";
import { useModelsCollection } from "./useModelsCollection";

/**
 * Models page composition root. Collection/cache, dialog CRUD, enablement,
 * deletion, strategy defaults, and metrics keep their own lifecycles.
 */
export function useModelsPageData(revision: number) {
  const collection = useModelsCollection(revision);
  const dialog = useModelDialogMutations({
    commitModels: collection.commitModels,
    loadbalanceStrategies: collection.loadbalanceStrategies,
    refreshStrategiesAfterDialogClose:
      collection.refreshStrategiesAfterDialogClose,
  });
  const strategyDefaults = useLoadbalanceStrategyDefaults({
    dialogSessionRef: dialog.modelDialogSessionRef,
    publishLoadbalanceStrategies: collection.publishLoadbalanceStrategies,
    readSortedLoadbalanceStrategies:
      collection.readSortedLoadbalanceStrategies,
    replaceLoadbalanceStrategies: collection.replaceLoadbalanceStrategies,
    setFormData: dialog.setFormData,
  });
  const enablement = useModelEnablementMutations({
    commitModels: collection.commitModels,
  });
  const deletion = useModelDeletion({ commitModels: collection.commitModels });
  const metrics = useModelMetrics24h(collection.models);

  return {
    deleteTarget: deletion.deleteTarget,
    editingModel: dialog.editingModel,
    formData: dialog.formData,
    formError: dialog.formError,
    handleDelete: deletion.handleDelete,
    handleModelCreated: dialog.handleModelCreated,
    createDialogOpen: dialog.createDialogOpen,
    setCreateDialogOpen: dialog.handleSetCreateDialogOpen,
    handleCreateLoadbalanceStrategyDefaults:
      strategyDefaults.handleCreateLoadbalanceStrategyDefaults,
    handleOpenDialog: dialog.handleOpenDialog,
    handleSubmit: dialog.handleSubmit,
    isDialogOpen: dialog.isDialogOpen,
    loadbalanceStrategies: collection.loadbalanceStrategies,
    loadbalanceStrategyDefaultsCreating:
      strategyDefaults.loadbalanceStrategyDefaultsCreating,
    loading: collection.loading,
    loadError: collection.loadError,
    metricsFailed: metrics.metricsFailed,
    metricsLoading: metrics.metricsLoading,
    modelMetrics24h: metrics.modelMetrics24h,
    modelSpend30dMicros: metrics.modelSpend30dMicros,
    models: collection.models,
    setDeleteTarget: deletion.setDeleteTarget,
    setModelEnabled: enablement.setModelEnabled,
    setModelsEnabled: enablement.setModelsEnabled,
    togglingModelIds: enablement.togglingModelIds,
    setFormData: dialog.setFormData,
    setIsDialogOpen: dialog.handleSetIsDialogOpen,
    setLoadbalanceStrategyId: dialog.setLoadbalanceStrategyId,
    retryLoad: collection.retryLoad,
  };
}
