import { Loader2, Pencil, Plus, Scale, Trash2 } from "lucide-react";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { useLocale } from "@/i18n/useLocale";
import { TypeBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  getAdaptiveRoutingObjectiveLabel,
  getAdaptiveRoutingObjectiveSummary,
  isAdaptiveLoadbalanceStrategy,
  isLegacyLoadbalanceStrategy,
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
  loadbalanceStrategyPreparingEditId: number | null;
  onCreate: () => void;
  onDelete: (strategy: LoadbalanceStrategy) => void;
  onEdit: (strategy: LoadbalanceStrategy) => Promise<void>;
}

export function LoadbalanceStrategiesTable({
  loadbalanceStrategies,
  loadbalanceStrategiesLoading,
  loadbalanceStrategyPreparingEditId,
  onCreate,
  onDelete,
  onEdit,
}: LoadbalanceStrategiesTableProps) {
  const { formatNumber, messages } = useLocale();
  const tableCopy = messages.loadbalanceStrategiesTable;
  const strategyCopy = messages.loadbalanceStrategyCopy;

  const getStrategyTypeLabel = (strategy: LoadbalanceStrategy) =>
    isLegacyLoadbalanceStrategy(strategy)
      ? strategyCopy.legacyFamilyLabel
      : strategyCopy.adaptiveFamilyLabel;

  const getStrategySummary = (strategy: LoadbalanceStrategy) => {
    if (isAdaptiveLoadbalanceStrategy(strategy)) {
      return getAdaptiveRoutingObjectiveSummary(strategy.routing_policy.routing_objective, strategyCopy);
    }

    return strategy.legacy_strategy_type === "single"
      ? `${strategyCopy.singleLabel} • ${strategyCopy.singleSummary}`
      : strategy.legacy_strategy_type === "fill-first"
        ? `${strategyCopy.fillFirstLabel} • ${strategyCopy.fillFirstSummary}`
        : `${strategyCopy.roundRobinLabel} • ${strategyCopy.roundRobinSummary}`;
  };

  const getRecoverySummary = (strategy: LoadbalanceStrategy) => {
    if (isAdaptiveLoadbalanceStrategy(strategy)) {
      return [
        tableCopy.adaptiveRoutingSummary(
          getAdaptiveRoutingObjectiveLabel(strategy.routing_policy.routing_objective, strategyCopy),
        ),
      ];
    }

    if (strategy.auto_recovery.mode === "disabled") {
      return [tableCopy.autoRecoveryDisabled];
    }

    const ban = strategy.auto_recovery.ban;

    return [
      tableCopy.autoRecoveryEnabled,
      tableCopy.statusCodes(strategy.auto_recovery.status_codes.join(", ")),
      tableCopy.cooldownSummary(
        formatNumber(strategy.auto_recovery.cooldown.base_seconds),
        formatNumber(strategy.auto_recovery.cooldown.max_cooldown_seconds),
      ),
      ban.mode === "off"
        ? tableCopy.banOff
        : ban.mode === "manual"
          ? tableCopy.banManualDismiss(
              formatNumber(ban.max_cooldown_strikes_before_ban),
            )
          : tableCopy.banTemporary(
              formatNumber(ban.max_cooldown_strikes_before_ban),
              formatNumber(ban.ban_duration_seconds),
            ),
    ];
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Scale className="h-4 w-4" />
              {tableCopy.title}
            </CardTitle>
            <CardDescription className="text-xs">{tableCopy.description}</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button type="button" size="sm" onClick={onCreate}>
              <Plus className="mr-2 h-3.5 w-3.5" />
              {tableCopy.addStrategy}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {loadbalanceStrategiesLoading ? (
          <div className="space-y-2">
            <div className="h-12 animate-pulse rounded-md bg-muted/50" />
            <div className="h-12 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : loadbalanceStrategies.length === 0 ? (
          <div className="rounded-md border border-dashed p-8 text-center">
            <p className="text-sm text-muted-foreground">{tableCopy.noStrategiesConfigured}</p>
          </div>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{tableCopy.name}</TableHead>
                  <TableHead>{tableCopy.type}</TableHead>
                  <TableHead>{tableCopy.recovery}</TableHead>
                  <TableHead>{tableCopy.attachedModels}</TableHead>
                  <TableHead className="text-right">{tableCopy.actions}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loadbalanceStrategies.map((strategy) => {
                  const isPreparingEdit = loadbalanceStrategyPreparingEditId === strategy.id;

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
                        <TypeBadge label={getStrategyTypeLabel(strategy)} intent="info" />
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                          {getRecoverySummary(strategy).map((summaryLine) => (
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
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <Pencil className="h-4 w-4" />
                              )}
                              <span className="sr-only">{tableCopy.edit}</span>
                            </IconActionButton>
                            <IconActionButton
                              size="icon"
                              destructive
                              onClick={() => onDelete(strategy)}
                            >
                              <Trash2 className="h-4 w-4" />
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
      </CardContent>
    </Card>
  );
}
