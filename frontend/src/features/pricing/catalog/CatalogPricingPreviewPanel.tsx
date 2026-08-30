import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import type { CatalogPricingPreviewResponse } from "@/lib/types";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorStatusBadge,
} from "@/shared/design-system";

import {
  catalogCardRoleLabel,
  catalogCommitBlockers,
  catalogIncompatibilityLabel,
  formatCatalogFetchedAt,
  orderedPlanRoles,
  PRICE_COMPONENTS,
  renderPriceComponent,
} from "./catalogPricingPresentation";

/**
 * The source-linked pricing preview, shared by the /route/pricing catalog
 * import and the model-detail action.
 *
 * It renders both ends of the mapping (the Prism model and the models.dev
 * offering), the fixed USD/PER_1M unit, all five price components per card, the
 * tier threshold, the catalog revision and fetch stamp, and every stable
 * incompatibility reason. An explicit `0` renders as `0`; only an absent
 * component renders the shared missing marker.
 */
export function CatalogPricingPreviewPanel({
  preview,
  showTargets = true,
}: {
  preview: CatalogPricingPreviewResponse;
  /** Model-detail already knows its own target, so it can hide the echo. */
  showTargets?: boolean;
}) {
  const { messages } = useLocale();
  const { format: formatInOperatorTimezone } = useTimezone();
  const copy = messages.modelCatalog;

  const actionBadge =
    preview.action === "create"
      ? { intent: "neutral" as const, label: copy.pricingCreateNotice }
      : preview.action === "drift"
        ? { intent: "degraded" as const, label: copy.pricingDriftTitle }
        : { intent: "healthy" as const, label: copy.pricingReuseNotice };

  const roles = orderedPlanRoles(preview);
  const blockers = catalogCommitBlockers(copy, preview, { confirmDrift: true });
  const fetchedAt = formatCatalogFetchedAt(
    copy,
    preview.fetched_at,
    formatInOperatorTimezone,
  );

  return (
    <div
      className="flex min-w-0 flex-col gap-[var(--density-inline-gap)]"
      data-testid="catalog-pricing-preview"
    >
      <OperatorInsetPanel
        title={copy.pricingOfferingLabel}
        description={preview.offering.name ?? preview.offering.catalog_model_id}
      >
        <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.pricingPrismModelLabel}
            </dt>
            <dd className="truncate">
              {preview.model ? (
                <span>
                  {preview.model.display_name}
                  <span className="ml-1 font-mono text-xs text-muted-foreground">
                    {preview.model.model_id}
                  </span>
                </span>
              ) : (
                <OperatorMissingValue className="text-sm" />
              )}
            </dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.pricingOfferingLabel}
            </dt>
            <dd className="truncate font-mono text-xs">
              {preview.offering.provider_id}/{preview.offering.catalog_model_id}
            </dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.pricingCatalogRevisionLabel}
            </dt>
            <dd
              className="truncate font-mono text-xs"
              title={preview.catalog_revision}
            >
              {preview.catalog_revision || (
                <OperatorMissingValue className="text-sm" />
              )}
            </dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.fetchedAtLabel}
            </dt>
            <dd className="truncate text-xs">{fetchedAt}</dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.pricingColumnRole}
            </dt>
            <dd className="truncate text-xs">
              {preview.plan.template_kind === "tiered"
                ? copy.pricingPlanKindTiered
                : copy.pricingPlanKindStandard}
            </dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">
              {copy.pricingUnitLabel}
            </dt>
            <dd className="truncate text-xs">
              {preview.reporting_currency_code} · {preview.catalog_currency}/
              {preview.pricing_unit}
            </dd>
          </div>
        </dl>

        <div className="flex flex-wrap items-center gap-2">
          <OperatorStatusBadge
            intent={actionBadge.intent}
            preserveLabel
            label={actionBadge.label}
          />
          {preview.plan.tier_input_tokens_above != null ? (
            <span className="text-xs text-muted-foreground">
              {copy.pricingTierThreshold(preview.plan.tier_input_tokens_above)}
            </span>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">
          {copy.pricingCatalogUnitNote(
            preview.catalog_currency,
            preview.pricing_unit,
          )}
        </p>
      </OperatorInsetPanel>

      {roles.length > 0 ? (
        <div className="min-w-0 overflow-x-auto rounded-md border border-border">
          <table className="w-full text-sm" data-testid="catalog-pricing-cards">
            <thead>
              <tr className="text-left text-xs text-muted-foreground">
                <th className="pr-2 font-normal">{copy.pricingColumnRole}</th>
                {PRICE_COMPONENTS.map((component) => (
                  <th
                    key={component.key}
                    className="px-2 text-right font-normal"
                  >
                    {component.label(copy)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {roles.map((role) => {
                const card = preview.plan.cards[role];
                return (
                  <tr key={role} className="border-t border-border">
                    <td className="whitespace-nowrap py-1 pr-2 font-mono text-xs">
                      {catalogCardRoleLabel(copy, role)}
                    </td>
                    {PRICE_COMPONENTS.map((component) => {
                      const value = card[component.key];
                      const rendered = renderPriceComponent(copy, value);
                      return (
                        <td
                          key={component.key}
                          className="px-2 py-1 text-right font-mono tabular-nums"
                        >
                          {rendered === null ? (
                            <OperatorMissingValue
                              reason={messages.honesty.noValue}
                            />
                          ) : (
                            rendered
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}

      {preview.plan.incompatibilities.length > 0 ? (
        <OperatorCallout intent="danger" title={copy.pricingIncompatibleTitle}>
          <div className="flex flex-col gap-1">
            <span>{copy.pricingIncompatibleDescription}</span>
            <ul className="list-inside list-disc">
              {preview.plan.incompatibilities.map((item) => (
                <li key={`${item.field}:${item.reason}`}>
                  <span className="font-mono text-xs">{item.field}</span>
                  <span className="ml-1">
                    {catalogIncompatibilityLabel(copy, item)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </OperatorCallout>
      ) : null}

      {showTargets && preview.targets.length > 0 ? (
        <OperatorInsetPanel title={copy.pricingTargetsLabel}>
          <ul className="flex flex-col gap-1 text-sm">
            {preview.targets.map((target) => (
              <li
                key={target.connection_id}
                className="flex min-w-0 items-center gap-2"
              >
                <span className="truncate">
                  {target.name ??
                    target.endpoint_name ??
                    copy.pricingTargetNameFallback(target.connection_id)}
                </span>
                <span className="font-mono text-xs text-muted-foreground">
                  {target.pricing_template_id ?? copy.valueAbsent}
                </span>
              </li>
            ))}
          </ul>
        </OperatorInsetPanel>
      ) : null}

      {!preview.committable && blockers.length > 0 ? (
        <OperatorCallout
          intent="warning"
          title={copy.pricingCommitBlockersTitle}
          description={blockers.join("；")}
        />
      ) : null}
    </div>
  );
}

export default CatalogPricingPreviewPanel;
