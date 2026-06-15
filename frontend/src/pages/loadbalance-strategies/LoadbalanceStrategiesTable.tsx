import { Loader2, Pencil, Plus, Scale, Sparkles, Trash2 } from "lucide-react";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { OperatorEmptyState, OperatorSectionCard, OperatorTypeBadge } from "@/shared/design-system";
import {
  getLegacyLoadbalanceStrategySummary,
  getLoadbalanceStrategyTypeLabel,
} from "@/lib/loadbalanceRoutingPolicy";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { LoadbalanceStrategy } from "@/lib/types";

interface LoadbalanceStrategiesTableProps {
  loadbalanceStrategies: LoadbalanceStrategy[];
  loadbalanceStrategiesLoading: boolean;
  loadbalanceStrategyDefaultsCreating: boolean;
  loadbalanceStrategyPreparingEditId: number | null;
  onCreate: () => void;
  onCreateDefaults: () => Promise<void>;
  onDelete: (strategy: LoadbalanceStrategy) => void;
  onEdit: (strategy: LoadbalanceStrategy) => Promise<void>;
}

export function LoadbalanceStrategiesTable({
  loadbalanceStrategies,
  loadbalanceStrategiesLoading,
  loadbalanceStrategyDefaultsCreating,
  loadbalanceStrategyPreparingEditId,
  onCreate,
  onCreateDefaults,
  onDelete,
  onEdit,
}: LoadbalanceStrategiesTableProps) {
  const { formatNumber, messages } = useLocale();
  const tableCopy = messages.loadbalanceStrategiesTable;
  const strategyCopy = messages.loadbalanceStrategyCopy;

  const getStrategySummary = (strategy: LoadbalanceStrategy) => {
    const typeLabel = getLoadbalanceStrategyTypeLabel(strategy, strategyCopy);
    const typeSummary = getLegacyLoadbalanceStrategySummary(strategy.legacy_strategy_type, strategyCopy);
    return `${typeLabel} • ${typeSummary}`;
  };

  const getBanPolicySummary = (strategy: LoadbalanceStrategy) => {
    const failureStatusCodes =
      strategy.failure_status_codes.length > 0
        ? strategy.failure_status_codes.join(", ")
        : messages.common.unavailable;

    return [
      tableCopy.statusCodes(failureStatusCodes),
      tableCopy.retryPolicySummary(
        formatNumber(strategy.retry_base_delay_ms),
        formatNumber(strategy.retry_max_delay_ms),
        formatNumber(strategy.cycle_retry_attempt_limit),
        formatNumber(strategy.retry_backoff_multiplier, { maximumFractionDigits: 2 }),
        formatNumber(strategy.retry_jitter_ratio, { maximumFractionDigits: 2 }),
      ),
      strategy.ban_mode === "off"
        ? tableCopy.banOff
        : strategy.ban_mode === "until_reset"
          ? tableCopy.banUntilResetPolicy(
              formatNumber(strategy.ban_cumulative_retry_attempt_threshold),
            )
          : tableCopy.banTemporaryPolicy(
              formatNumber(strategy.ban_cumulative_retry_attempt_threshold),
              formatNumber(strategy.ban_duration_seconds),
            ),
    ];
  };

  return (
    <OperatorSectionCard
      icon={<Scale />}
      title={tableCopy.title}
      description={tableCopy.description}
      actions={(
        <div className="flex items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={loadbalanceStrategyDefaultsCreating}
            onClick={() => {
              void onCreateDefaults();
            }}
          >
            {loadbalanceStrategyDefaultsCreating ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <Sparkles data-icon="inline-start" />
            )}
            {tableCopy.createDefaults}
          </Button>
          <Button type="button" size="sm" disabled={loadbalanceStrategyDefaultsCreating} onClick={onCreate}>
            <Plus data-icon="inline-start" />
            {tableCopy.addStrategy}
          </Button>
        </div>
      )}
      contentClassName="flex flex-col gap-4"
    >
      {loadbalanceStrategiesLoading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-12 rounded-md" />
          <Skeleton className="h-12 rounded-md" />
        </div>
      ) : loadbalanceStrategies.length === 0 ? (
        <OperatorEmptyState
          title={tableCopy.noStrategiesConfigured}
          action={(
            <div className="flex flex-col items-center justify-center gap-2 sm:flex-row">
              <Button
                type="button"
                disabled={loadbalanceStrategyDefaultsCreating}
                onClick={() => {
                  void onCreateDefaults();
                }}
              >
                {loadbalanceStrategyDefaultsCreating ? (
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                ) : (
                  <Sparkles data-icon="inline-start" />
                )}
                {tableCopy.createDefaults}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={loadbalanceStrategyDefaultsCreating}
                onClick={onCreate}
              >
                <Plus data-icon="inline-start" />
                {tableCopy.addStrategy}
              </Button>
            </div>
          )}
        />
      ) : (
          <div className="operator-table-shell overflow-hidden rounded-lg border border-outline-variant">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{tableCopy.name}</TableHead>
                  <TableHead>{tableCopy.type}</TableHead>
                  <TableHead>{tableCopy.banPolicy}</TableHead>
                  <TableHead>{tableCopy.attachedModels}</TableHead>
                  <TableHead className="text-right">{tableCopy.actions}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loadbalanceStrategies.map((strategy) => {
                  const isPreparingEdit = loadbalanceStrategyPreparingEditId === strategy.id;
                  const strategyTypeLabel = getLoadbalanceStrategyTypeLabel(strategy, strategyCopy);

                  return (
                    <TableRow key={strategy.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">{strategy.name}</span>
                          <span className="text-xs text-muted-foreground">
                            {getStrategySummary(strategy)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <OperatorTypeBadge label={strategyTypeLabel} intent="info" />
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                          {getBanPolicySummary(strategy).map((summaryLine) => (
                            <span key={summaryLine}>{summaryLine}</span>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm tabular-nums">
                          {formatNumber(strategy.attached_model_count)}
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-2">
                          <IconActionGroup>
                            <IconActionButton
                              size="icon"
                              disabled={isPreparingEdit}
                              onClick={() => {
                                void onEdit(strategy);
                              }}
                            >
                              {isPreparingEdit ? (
                                <Loader2 className="animate-spin" />
                              ) : (
                                <Pencil />
                              )}
                              <span className="sr-only">{tableCopy.edit}</span>
                            </IconActionButton>
                            <IconActionButton
                              size="icon"
                              destructive
                              onClick={() => onDelete(strategy)}
                            >
                              <Trash2 />
                              <span className="sr-only">{messages.settingsDialogs.delete}</span>
                            </IconActionButton>
                          </IconActionGroup>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
    </OperatorSectionCard>
  );
}
