import { useEffect, useMemo, useRef, useState } from "react"
import { ArrowRight, Plus } from "lucide-react"
import { Link } from "@tanstack/react-router"
import { Button } from "@/components/ui/button"
import { DeleteLoadbalanceStrategyDialog } from "@/pages/loadbalance-strategies/DeleteLoadbalanceStrategyDialog"
import { LoadbalanceStrategiesTable } from "@/pages/loadbalance-strategies/LoadbalanceStrategiesTable"
import { StrategyPreviewTimeline } from "@/pages/loadbalance-strategies/StrategyPreviewTimeline"
import { useLocale } from "@/i18n/useLocale"
import type { LoadbalanceStrategy } from "@/lib/types"
import {
  OperatorKpiCard,
  OperatorPageHeader,
  OperatorPageShell,
} from "@/shared/design-system"
import { PaginationLiveStatus } from "@/shared/table/paginationControls"
import { BanPolicyDialog } from "./BanPolicyDialog"
import { useBanPoliciesFeatureData } from "./useBanPoliciesFeatureData"

export function BanPoliciesFeaturePage() {
  const { formatNumber, messages } = useLocale()
  const data = useBanPoliciesFeatureData(0)
  const copy = messages.routingStrategyTable
  const [selected, setSelected] = useState<LoadbalanceStrategy | null>(null)
  const [announcement, setAnnouncement] = useState<string | null>(null)
  const previewRef = useRef<HTMLDivElement>(null)

  // 推演卡在折线以下：选中后既不滚动也不播报，会被当成「没反应」。
  const selectStrategy = (strategy: LoadbalanceStrategy) => {
    setSelected(strategy)
    setAnnouncement(copy.previewAnnounced(strategy.name))
  }

  // 滚动必须等这一次渲染提交完：在点击处理里量到的还是选中之前的布局。
  useEffect(() => {
    if (!selected) return
    previewRef.current?.scrollIntoView({ block: "nearest" })
  }, [selected])

  const deleteImpactStrategyId = (data.deleteConfirm ?? data.displayDelete)?.id ?? null

  const stats = useMemo(() => {
    const strategies = data.strategiesFragment.data ?? []
    const defaultStrategy = strategies.find((strategy) => strategy.is_default) ?? null
    return {
      total: strategies.length,
      defaultName: defaultStrategy?.name ?? null,
      banEnabled: strategies.filter((strategy) => strategy.ban_mode !== "off").length,
      unbound: strategies.filter((strategy) => strategy.attached_model_count === 0).length,
    }
  }, [data.strategiesFragment.data])

  return (
    <OperatorPageShell data-testid="ban-policies-feature-page">
      <OperatorPageHeader
        title={messages.loadbalanceStrategiesPage.title}
        description={messages.loadbalanceStrategiesPage.description}
        actions={
          <>
            <Button asChild variant="outline" size="sm">
              <Link to="/observe/routing-health">
                {messages.loadbalanceStrategiesPage.viewRoutingHealth}
                <ArrowRight data-icon="inline-end" />
              </Link>
            </Button>
            <Button type="button" size="sm" onClick={data.openCreate}>
              <Plus data-icon="inline-start" />
              {messages.routingStrategyTable.addStrategy}
            </Button>
          </>
        }
      />

      <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4">
        <OperatorKpiCard label={copy.kpiTotal} value={formatNumber(stats.total)} detail={copy.kpiTotalDetail} />
        <OperatorKpiCard
          label={copy.kpiDefault}
          value={stats.defaultName ?? copy.kpiDefaultNone}
          detail={copy.kpiDefaultDetail}
        />
        <OperatorKpiCard label={copy.kpiBanEnabled} value={formatNumber(stats.banEnabled)} detail={copy.kpiBanEnabledDetail} />
        <OperatorKpiCard label={copy.kpiUnbound} value={formatNumber(stats.unbound)} detail={copy.kpiUnboundDetail} />
      </div>

      <PaginationLiveStatus message={announcement} />

      <LoadbalanceStrategiesTable
        onSelect={selectStrategy}
        selectedId={selected?.id ?? null}
        fragment={data.strategiesFragment}
        defaultsCompleteness={data.defaultsCompleteness}
        defaultsCreating={data.defaultsCreating}
        defaultsError={data.defaultsError}
        preparingEditId={data.preparingEditId}
        setDefaultState={data.setDefaultState}
        impactStates={data.impactStates}
        onCreateDefaults={data.createDefaults}
        onEdit={data.openEdit}
        onDelete={data.openDelete}
        onSetDefault={data.setDefault}
        onClearSetDefaultError={data.clearSetDefaultError}
        onToggleImpact={data.toggleImpact}
        onLoadMoreImpact={data.loadMoreImpact}
        onRetryImpact={data.retryImpact}
        onRetry={data.refreshStrategies}
      />

      <div ref={previewRef}>
        <StrategyPreviewTimeline key={selected?.id ?? "none"} strategy={selected} />
      </div>

      <BanPolicyDialog
        editingStrategy={data.editingStrategy}
        initialValues={data.formValues}
        onClose={() => data.setDialogOpen(false)}
        onOpenChange={data.setDialogOpen}
        onSave={data.save}
        open={data.dialogOpen}
        saving={data.saving}
        saveError={data.saveError}
      />
      <DeleteLoadbalanceStrategyDialog
        deleteLoadbalanceStrategyConfirm={data.deleteConfirm}
        displayedDeleteLoadbalanceStrategyConfirm={data.displayDelete}
        loadbalanceStrategyDeleting={data.deleting}
        loadbalanceStrategyDeleteError={data.deleteError}
        onClose={data.closeDelete}
        onDelete={data.deleteStrategy}
        open={data.deleteConfirm !== null}
        // 阻塞删除的绑定名单与表格行内展开共用同一个分页器；retryImpact 在
        // 还没读过时取的就是第一页，读失败后重复的是上一次的游标。
        impactState={deleteImpactStrategyId !== null ? data.impactStates[deleteImpactStrategyId] : undefined}
        onLoadAttachedModels={data.retryImpact}
        onLoadMoreAttachedModels={data.loadMoreImpact}
      />
    </OperatorPageShell>
  )
}

export default BanPoliciesFeaturePage
