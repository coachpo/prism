import { Navigate, useNavigate, useParams } from "react-router-dom";
import { Skeleton } from "@/components/ui/skeleton";
import { ModelDetailHeader } from "./model-detail/ModelDetailHeader";
import { ModelSettingsDialog } from "./model-detail/ModelSettingsDialog";
import { OverviewCards } from "./model-detail/OverviewCards";
import { ProxyTargetsCard } from "./model-detail/ProxyTargetsCard";
import { getModelDetailPath } from "./model-detail/modelDetailMetricsAndPaths";
import { useModelDetailData } from "./model-detail/useModelDetailData";
import { useModelDetailPageShell } from "./model-detail/useModelDetailPageShell";

export function ProxyModelDetailPage() {
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
    editLoadbalanceStrategyId,
    setEditLoadbalanceStrategyId,
    spending,
    spendingLoading,
    spendingCurrencySymbol,
    spendingCurrencyCode,
    proxyTargetOptions,
    proxyTargetSummary,
    handleEditModelSubmit,
    handleSaveProxyTargets,
  } = useModelDetailData(id);

  if (loading) {
    return (
      <div className="space-y-[var(--density-page-gap)]">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8 rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-[120px] rounded-xl" />
        <Skeleton className="h-[280px] rounded-xl" />
      </div>
    );
  }

  if (!model) return null;

  if (model.model_type !== "proxy") {
    return <Navigate to={getModelDetailPath(model)} replace />;
  }

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
        proxyTargetSummary={proxyTargetSummary}
        onViewRequestLogs={() => navigateToRequestLogs(model.model_id)}
      />

      <ProxyTargetsCard
        availableTargets={proxyTargetOptions}
        proxyTargets={model.proxy_targets}
        saving={false}
        onSave={handleSaveProxyTargets}
      />

      <ModelSettingsDialog
        editLoadbalanceStrategyId={editLoadbalanceStrategyId}
        isOpen={isEditModelDialogOpen}
        loadbalanceStrategies={loadbalanceStrategies}
        onOpenChange={setIsEditModelDialogOpen}
        model={model}
        vendors={vendors}
        setEditLoadbalanceStrategyId={setEditLoadbalanceStrategyId}
        handleEditModelSubmit={handleEditModelSubmit}
      />
    </div>
  );
}
