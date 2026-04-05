import { useMemo, useState } from "react";
import { ChevronDown, Eye, Plus, Server, Trash2 } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { CopyButton } from "@/components/CopyButton";
import { EmptyState } from "@/components/EmptyState";
import { VendorIcon } from "@/components/VendorIcon";
import { useLocale } from "@/i18n/useLocale";
import { isKnownUnknownVendorLabel } from "@/i18n/staticMessages";
import {
  IconActionButton,
  IconActionGroup,
} from "@/components/IconActionGroup";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { formatMoneyMicros } from "@/lib/costing";
import type { ModelConfigListItem } from "@/lib/types";
import { getAdaptiveRoutingObjectiveLabel } from "@/lib/loadbalanceRoutingPolicy";
import { cn, formatApiFamily } from "@/lib/utils";
import {
  formatLatencyForDisplay,
  getModelDetailPath,
} from "../model-detail/modelDetailMetricsAndPaths";
import type { ModelDerivedMetric } from "./modelTableContracts";

const TYPE_ORDER: ModelConfigListItem["model_type"][] = ["native", "proxy"];
const UNKNOWN_VENDOR_KEY = "unknown-vendor";
const STATUS_ORDER = [
  {
    key: "enabled",
    matches: (model: ModelConfigListItem) => model.is_enabled,
  },
  {
    key: "disabled",
    matches: (model: ModelConfigListItem) => !model.is_enabled,
  },
] as const;

type Props = {
  filtered: ModelConfigListItem[];
  handleOpenDialog: (model?: ModelConfigListItem) => void;
  metricsLoading: boolean;
  modelMetrics24h: Record<number, ModelDerivedMetric>;
  modelSpend30dMicros: Record<number, number>;
  search: string;
  setDeleteTarget: (model: ModelConfigListItem) => void;
};

type SharedRenderProps = Pick<
  Props,
  | "metricsLoading"
  | "modelMetrics24h"
  | "modelSpend30dMicros"
  | "setDeleteTarget"
> & {
  onNavigate: (model: Pick<ModelConfigListItem, "id" | "model_type">) => void;
};

type VendorGroup = {
  groupKey: string;
  models: ModelConfigListItem[];
  vendor: {
    key?: string | null;
    name?: string | null;
    icon_key?: string | null;
  };
  vendorLabel: string;
  isUnknown: boolean;
};

function resolveApiFamily(model: ModelConfigListItem) {
  return model.api_family ?? "openai";
}

function isUnknownVendor(model: ModelConfigListItem) {
  return !model.vendor || model.vendor.key === UNKNOWN_VENDOR_KEY || isKnownUnknownVendorLabel(model.vendor.name);
}

function resolveVendorName(model: ModelConfigListItem, unknownVendorLabel: string) {
  if (isUnknownVendor(model)) {
    return unknownVendorLabel;
  }

  return model.vendor?.name?.trim() || model.vendor?.key?.trim() || unknownVendorLabel;
}

function orderVendorModels(models: ModelConfigListItem[]) {
  return TYPE_ORDER.flatMap((modelType) =>
    STATUS_ORDER.flatMap(({ matches }) => models.filter((model) => model.model_type === modelType && matches(model)))
  );
}

function groupModels(filtered: ModelConfigListItem[], unknownVendorLabel: string): VendorGroup[] {
  const groups = new Map<string, VendorGroup>();

  filtered.forEach((model) => {
    const unknown = isUnknownVendor(model);
    const vendorLabel = resolveVendorName(model, unknownVendorLabel);
    const vendor = unknown
      ? { key: UNKNOWN_VENDOR_KEY, name: vendorLabel, icon_key: null }
      : {
          key: model.vendor?.key,
          name: model.vendor?.name,
          icon_key: model.vendor?.icon_key,
        };
    const vendorIdentity = unknown
      ? UNKNOWN_VENDOR_KEY
      : model.vendor?.id
        ? `vendor:${model.vendor.id}`
        : `vendor:${model.vendor?.key ?? model.vendor?.name ?? vendorLabel}`;

    const existingGroup = groups.get(vendorIdentity);
    if (existingGroup) {
      existingGroup.models.push(model);
      return;
    }

    groups.set(vendorIdentity, {
      groupKey: vendorIdentity,
      models: [model],
      vendor,
      vendorLabel,
      isUnknown: unknown,
    });
  });

  return [...groups.values()]
    .sort((left, right) => {
      if (left.isUnknown !== right.isUnknown) {
        return left.isUnknown ? 1 : -1;
      }

      return left.vendorLabel.localeCompare(right.vendorLabel);
    })
    .map((group) => ({
      ...group,
      models: orderVendorModels(group.models),
    }));
}

function getSuccessRateClass(successRate: number | null) {
  return cn(
    successRate === null
      ? "text-muted-foreground"
      : successRate >= 95
        ? "text-emerald-600 dark:text-emerald-400"
        : successRate >= 80
          ? "text-amber-600 dark:text-amber-400"
          : "text-red-600 dark:text-red-400"
  );
}

function CompactMetaBadge({ children }: { children: string }) {
  return (
    <Badge
      variant="outline"
      className="h-5 max-w-full rounded-full border-border/70 bg-muted/30 px-2 text-[10px] font-medium text-muted-foreground"
    >
      <span className="block max-w-full truncate">{children}</span>
    </Badge>
  );
}

function InlineMetaDivider() {
  return (
    <span aria-hidden="true" className="text-muted-foreground/45">
      |
    </span>
  );
}

function getProxyTargetSummary(model: ModelConfigListItem, copy: { noProxyTargets: string; targetsFirst: (count: string, first: string) => string }, formatNumber: (value: number) => string) {
  const proxyTargets = model.proxy_targets ?? [];
  const firstTarget = proxyTargets[0]?.target_model_id;

  if (!firstTarget) {
    return copy.noProxyTargets;
  }

  return copy.targetsFirst(formatNumber(proxyTargets.length), firstTarget);
}

function ModelRow({
  metricsLoading,
  model,
  modelMetrics24h,
  modelSpend30dMicros,
  onNavigate,
  setDeleteTarget,
}: SharedRenderProps & { model: ModelConfigListItem }) {
  const { formatNumber, locale, messages } = useLocale();
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const metrics24h = modelMetrics24h[model.id];
  const successRate = metrics24h?.success_rate ?? null;
  const requestCount = metrics24h?.request_count_24h ?? 0;
  const p95LatencyMs = metrics24h?.p95_latency_ms ?? null;
  const spend30dMicros = modelSpend30dMicros[model.id] ?? 0;
  const title = model.display_name || model.model_id;
  const proxyTargetSummary = getProxyTargetSummary(model, copy, formatNumber);
  const apiFamilyLabel = formatApiFamily(resolveApiFamily(model));
  const vendorLabel = resolveVendorName(model, copy.unknownVendor);
  const typeLabel = model.model_type === "proxy" ? detailCopy.typeProxy : detailCopy.typeNative;
  const typeIntent = model.model_type === "proxy" ? "accent" : "info";
  const statusLabel = model.is_enabled ? detailCopy.enabled : detailCopy.disabled;
  const statusIntent = model.is_enabled ? "success" : "muted";
  const showModelId = Boolean(model.display_name && model.display_name !== model.model_id);
  const strategyTypeLabel = model.loadbalance_strategy
    ? model.loadbalance_strategy.strategy_type === "adaptive"
      ? strategyCopy.adaptiveFamilyLabel
      : model.loadbalance_strategy.legacy_strategy_type === "single"
        ? strategyCopy.singleLabel
        : model.loadbalance_strategy.legacy_strategy_type === "fill-first"
          ? strategyCopy.fillFirstLabel
          : strategyCopy.roundRobinLabel
    : null;
  const strategySummaryLabel = model.loadbalance_strategy
    ? model.loadbalance_strategy.strategy_type === "adaptive"
      ? `${strategyCopy.adaptiveFamilyLabel} • ${getAdaptiveRoutingObjectiveLabel(model.loadbalance_strategy.routing_policy.routing_objective, strategyCopy)}`
      : strategyTypeLabel
    : null;
  const strategySummary = model.loadbalance_strategy
    ? `${model.loadbalance_strategy.name} · ${strategySummaryLabel}`
    : copy.strategyNotConfigured;
  const successRateText =
      metricsLoading && !metrics24h
        ? `… ${copy.successLabel}`
      : successRate === null
        ? `— ${copy.successLabel}`
        : `${formatNumber(successRate, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}% ${copy.successLabel}`;
  const p95LatencyText =
    metricsLoading
      ? "… P95"
      : `${formatLatencyForDisplay(p95LatencyMs)} P95`;
  const requestCountText =
    metricsLoading && !metrics24h
      ? `… ${copy.requestsShort}`
      : requestCount > 0
        ? `${formatNumber(requestCount)} ${copy.requestsShort}`
        : `— ${copy.requestsShort}`;
  const spend30dText =
    metricsLoading
      ? `… ${copy.spendShort}`
      : `${formatMoneyMicros(spend30dMicros, "$", undefined, 2, 6, locale as "en" | "zh-CN")} ${copy.spendShort}`;

  return (
    <div className="group flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/25">
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <Link
            to={getModelDetailPath(model)}
            className="min-w-0 max-w-full truncate rounded-md text-base font-semibold text-foreground outline-none transition-colors hover:text-primary focus-visible:ring-2 focus-visible:ring-ring/50"
            title={title}
          >
            {title}
          </Link>
          {showModelId ? (
            <span className="truncate font-mono text-xs text-muted-foreground" title={model.model_id}>
              {model.model_id}
            </span>
          ) : null}
          <CopyButton
            aria-label={detailCopy.copyModelIdAria(model.model_id)}
            className="h-5 w-5 rounded-md text-muted-foreground hover:text-foreground"
            errorMessage={messages.auth.loginFailed}
            label=""
            size="icon-xs"
            successMessage={messages.requestLogs.copy}
            targetLabel={detailCopy.modelIdLabel}
            value={model.model_id}
          />
        </div>

        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {model.model_type === "proxy" ? (
              <CompactMetaBadge>{proxyTargetSummary}</CompactMetaBadge>
            ) : null}

            {model.model_type === "native" ? <CompactMetaBadge>{strategySummary}</CompactMetaBadge> : null}
            <CompactMetaBadge>{vendorLabel}</CompactMetaBadge>
            <CompactMetaBadge>{apiFamilyLabel}</CompactMetaBadge>
            <TypeBadge label={typeLabel} intent={typeIntent} />
            <StatusBadge label={statusLabel} intent={statusIntent} />
          </div>

          <InlineMetaDivider />
          <span className="tabular-nums text-xs text-foreground/90">
            {copy.activeConnections(formatNumber(model.active_connection_count), formatNumber(model.connection_count))}
          </span>
          <InlineMetaDivider />
          <span
            className={cn(
              "tabular-nums text-xs",
              metricsLoading && !metrics24h ? "text-muted-foreground" : getSuccessRateClass(successRate)
            )}
          >
            {successRateText}
          </span>
          <InlineMetaDivider />
          <span className="tabular-nums text-xs text-muted-foreground">{p95LatencyText}</span>
          <InlineMetaDivider />
          <span className="tabular-nums text-xs text-muted-foreground">{requestCountText}</span>
          <InlineMetaDivider />
          <span className="tabular-nums text-xs text-foreground/90">{spend30dText}</span>
        </div>
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 pt-0.5">
        <IconActionGroup>
            <IconActionButton
              aria-label={copy.viewModelDetails(title)}
              size="icon-sm"
              onClick={() => onNavigate(model)}
            >
              <Eye className="h-3.5 w-3.5" />
            </IconActionButton>
          <IconActionButton
            aria-label={copy.deleteModelDescription(title)}
            size="icon-sm"
            destructive
            onClick={() => setDeleteTarget(model)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </IconActionButton>
        </IconActionGroup>
      </div>
    </div>
  );
}

function VendorSection({
  group,
  isExpanded,
  metricsLoading,
  modelMetrics24h,
  modelSpend30dMicros,
  onNavigate,
  onOpenChange,
  search,
  setDeleteTarget,
}: SharedRenderProps & {
  group: VendorGroup;
  isExpanded: boolean;
  onOpenChange: (isOpen: boolean) => void;
  search: string;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const modelCountLabel =
    copy.modelCount(formatNumber(group.models.length));
  const isSearchActive = search.trim().length > 0;

  return (
    <Collapsible open={isExpanded} onOpenChange={onOpenChange}>
      <Card className="gap-0 overflow-hidden border-border/70 bg-card py-0 shadow-none">
        <CollapsibleTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            disabled={isSearchActive}
            className="h-auto w-full justify-between gap-3 rounded-none px-4 py-3 hover:bg-muted/30 disabled:cursor-default disabled:opacity-100 disabled:hover:bg-transparent"
          >
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border/70 bg-muted/35 text-foreground">
                <VendorIcon vendor={group.vendor} size={18} decorative />
              </div>

              <div className="min-w-0 text-left">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-semibold text-foreground">{group.vendorLabel}</span>
                  <Badge
                    variant="outline"
                    className="rounded-full bg-muted/30 px-2 py-0.5 text-[10px] text-muted-foreground"
                  >
                    {modelCountLabel}
                  </Badge>
                </div>
              </div>
            </div>

            <ChevronDown
              className={cn(
                "h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200",
                isExpanded && "rotate-180"
              )}
            />
          </Button>
        </CollapsibleTrigger>

        <CollapsibleContent className="border-t border-border/60">
          <div className="divide-y divide-border/60">
            {group.models.map((model) => (
              <ModelRow
                key={model.id}
                metricsLoading={metricsLoading}
                model={model}
                modelMetrics24h={modelMetrics24h}
                modelSpend30dMicros={modelSpend30dMicros}
                onNavigate={onNavigate}
                setDeleteTarget={setDeleteTarget}
              />
            ))}
          </div>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  );
}

export function ModelsTable({
  filtered,
  handleOpenDialog,
  metricsLoading,
  modelMetrics24h,
  modelSpend30dMicros,
  search,
  setDeleteTarget,
}: Props) {
  const { messages } = useLocale();
  const navigate = useNavigate();
  const vendorGroups = useMemo(() => groupModels(filtered, messages.modelsUi.unknownVendor), [filtered, messages.modelsUi.unknownVendor]);
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({});

  if (filtered.length === 0) {
    return (
      <EmptyState
        icon={<Server className="h-6 w-6" />}
        title={
          search ? messages.modelsUi.noModelsMatchSearch : messages.modelsUi.noModelsConfigured
        }
        description={
          search ? messages.modelsUi.tryDifferentModelNameOrId : messages.modelsUi.createFirstModel
        }
        action={
          !search ? (
            <Button size="sm" onClick={() => handleOpenDialog()}>
              <Plus className="mr-1.5 h-4 w-4" />
              {messages.modelsPage.newModel}
            </Button>
          ) : undefined
        }
      />
    );
  }

  return (
    <div className="grid gap-3 p-4">
      {vendorGroups.map((group) => (
        <VendorSection
          key={group.groupKey}
          group={group}
          isExpanded={search.trim().length > 0 || (expandedGroups[group.groupKey] ?? true)}
          metricsLoading={metricsLoading}
          modelMetrics24h={modelMetrics24h}
          modelSpend30dMicros={modelSpend30dMicros}
          onNavigate={(model) => navigate(getModelDetailPath(model))}
          onOpenChange={(isOpen) =>
            setExpandedGroups((currentState) => ({
              ...currentState,
              [group.groupKey]: isOpen,
            }))
          }
          search={search}
          setDeleteTarget={setDeleteTarget}
        />
      ))}
    </div>
  );
}
