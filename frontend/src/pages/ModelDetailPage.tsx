import { useParams, useNavigate } from "react-router-dom";
import { Skeleton } from "@/components/ui/skeleton";
import { useModelDetailData } from "./model-detail/useModelDetailData";
import { useModelDetailPageShell } from "./model-detail/useModelDetailPageShell";
import { ModelDetailHeader } from "./model-detail/ModelDetailHeader";
import { OverviewCards } from "./model-detail/OverviewCards";
import { ConnectionDialog } from "./model-detail/ConnectionDialog";
import { ModelSettingsDialog } from "./model-detail/ModelSettingsDialog";
import { AccessTargetsEditor } from "./models/AccessTargetsEditor";
import { accessTargetToMutation } from "./models/modelFormState";
import { isOwnedConnectionTarget } from "./model-detail/useModelDetailDataSupport";

export function ModelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { navigateBackToModels, navigateToRequestLogs } = useModelDetailPageShell(navigate);

  const {
    model,
    loading,
    loadbalanceStrategies,
    vendors,
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    formData,
    setFormData,
    setLoadbalanceStrategyId,
    targetConnectionsForApiFamily,
    targetModelsForApiFamily,
    targetEditorError,
    spending,
    spendingLoading,
    spendingCurrencySymbol,
    spendingCurrencyCode,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    healthCheckingIds,
    dialogTestingConnection,
    dialogTestResult,
    clearDialogTestResult,
    globalEndpoints,
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    accessTargetSummary,
    endpointSourceDefaultName,
    openConnectionDialog,
    handleConnectionSubmit,
    handleHealthCheck,
    handleDialogTestConnection,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleUpdateModelTarget,
    handleDeleteAccessTarget,
    handleEditModelSubmit,
    pricingTemplates,
  } = useModelDetailData(id);

  if (loading) {
    return (
      <div className="space-y-[var(--density-page-gap)]">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8 rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-[120px] rounded-xl" />
        <Skeleton className="h-[400px] rounded-xl" />
      </div>
    );
  }

  if (!model) return null;

  const modelConfigId = id ? Number.parseInt(id, 10) : undefined;
  const isConnectionTargetMutable = (connectionId: number) =>
    isOwnedConnectionTarget(model, modelConfigId, connectionId);

  return (
    <div className="space-y-[var(--density-page-gap)] pb-2">
      <ModelDetailHeader
        model={model}
        onBack={navigateBackToModels}
        onEditModel={() => setIsEditModelDialogOpen(true)}
      />

      <OverviewCards
        model={model}
        spending={spending}
        spendingLoading={spendingLoading}
        spendingCurrencySymbol={spendingCurrencySymbol}
        spendingCurrencyCode={spendingCurrencyCode}
        accessTargetSummary={accessTargetSummary}
        onViewRequestLogs={() => navigateToRequestLogs(model.model_id)}
      />
      <AccessTargetsEditor
        apiFamilyLabel={model.api_family}
        accessTargets={model.access_targets
          .map(accessTargetToMutation)
          .filter((target): target is NonNullable<typeof target> => target !== null)}
        modelOptions={targetModelsForApiFamily}
        connectionOptions={targetConnectionsForApiFamily}
        error={targetEditorError}
        healthCheckingIds={healthCheckingIds}
        isConnectionTargetMutable={isConnectionTargetMutable}
        onAddTarget={handleAddAccessTarget}
        onCreateConnection={() => openConnectionDialog()}
        onDeleteTarget={handleDeleteAccessTarget}
        onEditConnection={openConnectionDialog}
        onHealthCheck={handleHealthCheck}
        onMoveTarget={handleMoveAccessTarget}
        onToggleTarget={handleToggleAccessTarget}
        onUpdateModelTarget={handleUpdateModelTarget}
        onChange={() => undefined}
      />

      <ConnectionDialog
        isOpen={isConnectionDialogOpen}
        onOpenChange={setIsConnectionDialogOpen}
        apiFamily={model.api_family}
        editingConnection={editingConnection}
        connectionForm={connectionForm}
        setConnectionForm={setConnectionForm}
        newEndpointForm={newEndpointForm}
        setNewEndpointForm={setNewEndpointForm}
        createMode={createMode}
        setCreateMode={setCreateMode}
        selectedEndpointId={selectedEndpointId}
        setSelectedEndpointId={setSelectedEndpointId}
        globalEndpoints={globalEndpoints}
        headerRows={headerRows}
        setHeaderRows={setHeaderRows}
        handleConnectionSubmit={handleConnectionSubmit}
        dialogTestingConnection={dialogTestingConnection}
        dialogTestResult={dialogTestResult}
        clearDialogTestResult={clearDialogTestResult}
        handleDialogTestConnection={handleDialogTestConnection}
        endpointSourceDefaultName={endpointSourceDefaultName}
        ownerCapabilityDefaults={{
          context_window_tokens: model.context_window_tokens,
          default_output_token_reserve: model.default_output_token_reserve,
          max_context_utilization: model.max_context_utilization,
          preferred_context_utilization_threshold: model.preferred_context_utilization_threshold,
        }}
        pricingTemplates={pricingTemplates}
      />

      <ModelSettingsDialog
        formData={formData}
        handleEditModelSubmit={handleEditModelSubmit}
        isOpen={isEditModelDialogOpen}
        loadbalanceStrategies={loadbalanceStrategies}
        model={model}
        targetEditorError={targetEditorError}
        targetModelsForApiFamily={targetModelsForApiFamily}
        onOpenChange={setIsEditModelDialogOpen}
        setFormData={setFormData}
        setLoadbalanceStrategyId={setLoadbalanceStrategyId}
        vendors={vendors}
      />
    </div>
  );
}
