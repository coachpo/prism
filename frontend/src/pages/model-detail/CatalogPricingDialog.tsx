import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import type { Messages } from "@/i18n/messages";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { pricingTemplates as pricingApi } from "@/lib/api/management_resources";
import type {
  CatalogPricingPreviewResponse,
  Connection,
} from "@/lib/types";
import { OperatorCallout, OperatorStatusBadge } from "@/shared/design-system";

/**
 * Terminal Target 价格生成对话框：基于 models.dev 目录价格预览、修订并原子
 * 赋给当前或显式选择的终端目标。默认只赋当前目标；多目标走既有双 CAS，
 * 任一冲突整笔回滚。
 */
export function CatalogPricingDialog({
  isOpen,
  modelConfigId,
  connectionId,
  connectionName,
  connections,
  onClose,
  onCommitted,
}: {
  isOpen: boolean;
  modelConfigId: number;
  connectionId: number | null;
  connectionName: string;
  connections: Connection[];
  onClose: () => void;
  onCommitted: (templateName: string, assignedCount: number) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  // The dialog mounts only while open, so initial state derives straight from
  // props; closing unmounts and discards drafts.
  const [selected, setSelected] = useState<Set<number>>(() => new Set(connectionId ? [connectionId] : []));
  const [confirmDrift, setConfirmDrift] = useState(false);
  // Settled-record pattern: pending/loading derive from "no settled result for
  // the current target set yet"; effects never call setState synchronously.
  const [settled, setSettled] = useState<{
    targetsKey: string;
    preview: CatalogPricingPreviewResponse | null;
    error: string | null;
  } | null>(null);
  const [committing, setCommitting] = useState(false);

  const targetsKey = useMemo(() => [...selected].sort((a, b) => a - b).join(","), [selected]);
  const loading = settled === null || settled.targetsKey !== targetsKey;
  const preview = !loading ? settled?.preview ?? null : null;
  const error = !loading ? settled?.error ?? null : null;

  // 目标集合变化即重新预览：preview_hash 绑定目标 CAS 快照，任何漂移都会在
  // 提交时被拒绝并要求重新预览。
  const loadPreview = useCallback(
    async (targetIds: number[]) => {
      const key = [...targetIds].sort((a, b) => a - b).join(",");
      try {
        const response = await pricingApi.catalogPreview({
          model_config_id: modelConfigId,
          connection_ids: targetIds,
        });
        setSettled({ targetsKey: key, preview: response, error: null });
      } catch (cause) {
        setSettled({ targetsKey: key, preview: null, error: cause instanceof Error ? cause.message : String(cause) });
      }
    },
    [modelConfigId],
  );

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      if (cancelled) return;
      await loadPreview([...selected]);
    })();
    return () => {
      cancelled = true;
    };
    // selected 的内容变化驱动重预览；序列化成稳定键避免无限循环。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadPreview, targetsKey]);

  const toggleTarget = (id: number) => {
    setSelected((current) => {
      if (current.has(id)) {
        if (id === connectionId) return current; // 默认当前目标不可取消
        const next = new Set(current);
        next.delete(id);
        return next;
      }
      const next = new Set(current);
      next.add(id);
      return next;
    });
  };

  const planCards = useMemo(() => {
    if (!preview) return [];
    const roles = Object.keys(preview.plan.cards).sort();
    return roles.map((role) => ({ role, card: preview.plan.cards[role] }));
  }, [preview]);

  const committable =
    Boolean(preview?.committable && preview.preview_hash) &&
    selected.size > 0 &&
    (!preview?.drift || confirmDrift);

  const handleCommit = async () => {
    if (!preview?.preview_hash || !preview.committable) return;
    setCommitting(true);
    try {
      await pricingApi.catalogCommit({
        schema_version: preview.schema_version,
        model_config_id: modelConfigId,
        connection_ids: [...selected],
        preview_hash: preview.preview_hash,
        expected_catalog_revision: preview.catalog_revision,
        confirm_drift: confirmDrift,
      });
      onCommitted(templateDisplayName(preview), selected.size);
      onClose();
    } catch (cause) {
      // 409 家族意味着状态漂移：错误进入 settled 记录展示，同时强制重新
      // 预览而不是猜测合并。
      const message = cause instanceof Error ? cause.message : String(cause);
      setSettled((current) => ({
        targetsKey: current?.targetsKey ?? targetsKey,
        preview: null,
        error: message,
      }));
      await loadPreview([...selected]);
    } finally {
      setCommitting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !committing && onClose()}>
      <DialogContent className="max-w-2xl" data-testid="catalog-pricing-dialog">
        <DialogHeader>
          <DialogTitle>
            {copy.pricingDialogTitlePrefix}
            {connectionName ? ` · ${connectionName}` : ""}
          </DialogTitle>
          <DialogDescription>{copy.pricingDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[65vh] flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
          {loading && <p className="text-sm text-muted-foreground">{copy.loadingText}</p>}
          {error && (
            <OperatorCallout intent="danger" title={copy.pricingLoadFailed}>
              <span>{error}</span>
            </OperatorCallout>
          )}

          {preview && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-sm">
                  {preview.offering.provider_id}/{preview.offering.catalog_model_id}
                </span>
                <OperatorStatusBadge
                  intent={preview.action === "drift" ? "degraded" : preview.action === "create" ? "neutral" : "healthy"}
                  preserveLabel
                  label={
                    preview.action === "create"
                      ? copy.pricingCreateNotice
                      : preview.action === "reuse"
                        ? copy.pricingReuseNotice
                        : copy.pricingDriftTitle
                  }
                />
              </div>
              <p className="text-xs text-muted-foreground">
                {copy.pricingCurrencyNote(preview.reporting_currency_code)}
              </p>

              <div className="rounded-md border p-3">
                <p className="text-sm font-medium">
                  {preview.plan.template_kind === "tiered"
                    ? copy.pricingPlanKindTiered
                    : copy.pricingPlanKindStandard}
                </p>
                {preview.plan.tier_input_tokens_above != null && (
                  <p className="text-xs text-muted-foreground">
                    {copy.pricingTierThreshold(preview.plan.tier_input_tokens_above)}
                  </p>
                )}
                <table className="mt-2 w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs text-muted-foreground">
                      <th className="pr-2 font-normal">{copy.pricingColumnRole}</th>
                      <th className="pr-2 font-normal">{copy.pricingColumnInput}</th>
                      <th className="pr-2 font-normal">{copy.pricingColumnOutput}</th>
                      <th className="pr-2 font-normal">{copy.pricingColumnCacheRead}</th>
                      <th className="font-normal">{copy.pricingColumnCacheWrite}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {planCards.map(({ role, card }) => (
                      <tr key={role}>
                        <td className="pr-2 font-mono text-xs">{cardRoleLabel(copy, role)}</td>
                        <td className="pr-2">{card.input_price}</td>
                        <td className="pr-2">{card.output_price}</td>
                        <td className="pr-2">{card.cached_input_price ?? copy.valueNotApplicable}</td>
                        <td>{card.cache_creation_price ?? copy.valueNotApplicable}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {preview.plan.incompatibilities.length > 0 && (
                <div className="rounded-md border border-destructive/40 p-3" role="alert">
                  <p className="text-sm font-medium text-destructive">{copy.pricingIncompatibleTitle}</p>
                  <ul className="mt-1 list-inside list-disc text-sm text-muted-foreground">
                    {preview.plan.incompatibilities.map((item) => (
                      <li key={`${item.field}:${item.reason}`}>
                        <span className="font-mono text-xs">{item.reason}</span>
                        <span className="ml-1">({item.field})</span>
                      </li>
                    ))}
                  </ul>
                  <p className="mt-1 text-xs">{copy.pricingIncompatibleDescription}</p>
                </div>
              )}

              {preview.drift && (
                <label className="flex items-start gap-2 rounded-md border border-warning-foreground/40 bg-warning-background/20 p-3 text-sm">
                  <Checkbox
                    checked={confirmDrift}
                    onCheckedChange={(checked) => setConfirmDrift(checked === true)}
                    data-testid="catalog-pricing-confirm-drift"
                  />
                  <span>{copy.pricingDriftConfirmLabel}</span>
                </label>
              )}
            </>
          )}

          <div className="flex flex-col gap-1">
            <p className="text-sm font-medium">{copy.pricingTargetsLabel}</p>
            <div className="flex flex-col gap-1">
              {(connections.length > 0 ? connections : []).map((connection) => (
                <label key={connection.id} className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={selected.has(connection.id)}
                    onCheckedChange={() => toggleTarget(connection.id)}
                    disabled={!isOpen}
                  />
                  <span className="truncate">
                    {connection.name ?? connection.endpoint?.name ?? copy.pricingTargetNameFallback(connection.id)}
                  </span>
                  {connection.id === connectionId && (
                    <OperatorStatusBadge intent="accent" preserveLabel label={copy.pricingCurrentTargetBadge} />
                  )}
                </label>
              ))}
            </div>
          </div>

          <div className="flex flex-col gap-1">
            <label className="sr-only" htmlFor="pricing-commit-note">
              commit
            </label>
            <Input id="pricing-commit-note" className="hidden" readOnly value="" tabIndex={-1} />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={committing}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            type="button"
            disabled={!committable || committing || loading}
            onClick={() => void handleCommit()}
            data-testid="catalog-pricing-submit"
          >
            {copy.pricingCommitAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function templateDisplayName(preview: CatalogPricingPreviewResponse): string {
  return preview.template?.name ?? `${preview.offering.provider_id}/${preview.offering.catalog_model_id}`;
}

function cardRoleLabel(copy: Messages["modelCatalog"], role: string): string {
  if (role === "standard") return copy.pricingPlanKindStandard;
  if (role === "tier_base") return copy.pricingRoleTierBase;
  if (role === "tier_above") return copy.pricingRoleTierAbove;
  if (role === "peak") return copy.pricingRolePeak;
  if (role === "offpeak") return copy.pricingRoleOffpeak;
  return role;
}
