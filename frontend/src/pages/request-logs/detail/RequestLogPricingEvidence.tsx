import { useLocale } from "@/i18n/useLocale";
import type {
  PricingCardRole,
  PricingProjection,
  PricingResolutionKind,
} from "@/lib/types/request-logs";
import type { PricingTemplateKind } from "@/lib/types/routing";
import {
  OperatorCallout,
  OperatorMissingValue,
} from "@/shared/design-system";
import { classifyPricingSelection } from "../pricingExplanation";
import { DetailRow } from "./requestLogDetailShared";

function kindLabel(
  kind: PricingTemplateKind | null,
  copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
) {
  switch (kind) {
    case "standard":
      return copy.pricingKindStandard;
    case "tiered":
      return copy.pricingKindTiered;
    case "peak_valley":
      return copy.pricingKindPeakValley;
    default:
      return null;
  }
}

function roleLabel(
  role: PricingCardRole,
  copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
) {
  switch (role) {
    case "standard":
      return copy.pricingCardStandard;
    case "tier_base":
      return copy.pricingCardTierBase;
    case "tier_above":
      return copy.pricingCardTierAbove;
    case "peak":
      return copy.pricingCardPeak;
    case "offpeak":
      return copy.pricingCardOffpeak;
  }
}

function resolutionLabel(
  resolution: PricingResolutionKind | null,
  copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
) {
  switch (resolution) {
    case "missing_component":
      return copy.pricingResolutionMissingComponent;
    case "currency_migration_required":
      return copy.pricingResolutionCurrencyMigration;
    case "unsupported_unit":
      return copy.pricingResolutionUnsupportedUnit;
    case "snapshot_incoherent":
      return copy.pricingResolutionSnapshotIncoherent;
    case "schedule_unresolved":
      return copy.pricingResolutionScheduleUnresolved;
    default:
      return copy.pricingResolutionUnknown;
  }
}

function snapshotValue(value: string | null) {
  return value === null ? "—" : value;
}

export function RequestLogPricingEvidence({
  pricing,
  formatTimestamp,
}: {
  pricing: PricingProjection;
  formatTimestamp: (iso: string) => string;
}) {
  const { messages, formatNumber } = useLocale();
  const copy = messages.requestLogs;
  const selection = classifyPricingSelection({
    state: pricing.pricing_selection_state,
    role: pricing.pricing_card_role,
    threshold: pricing.pricing_selector_threshold_tokens,
    basis: pricing.pricing_selector_basis_tokens,
  });
  const selectedRole =
    selection.kind === "selected" || selection.kind === "not_applicable"
      ? roleLabel(selection.role, copy)
      : null;
  const selectionValue = (() => {
    switch (selection.kind) {
      case "unavailable":
        return <OperatorMissingValue reason={copy.pricingSelectionUnavailable} />;
      case "not_evaluated":
        return copy.pricingSelectionNotEvaluated;
      case "not_applicable":
        return copy.pricingSelectionNotApplicable;
      case "selected":
        return copy.pricingSelectionSelected;
      case "unresolved":
        return (
          <OperatorCallout intent="warning">
            {copy.pricingSelectionUnresolved} · {resolutionLabel(pricing.pricing_resolution_kind, copy)}
          </OperatorCallout>
        );
    }
  })();

  return (
    <>
      <DetailRow label={copy.pricingTemplateKind}>
        {kindLabel(pricing.pricing_template_kind, copy) ?? (
          <OperatorMissingValue reason={copy.pricingSelectionUnavailable} />
        )}
      </DetailRow>
      <DetailRow label={copy.pricingSelection}>
        <div className="flex flex-col gap-1">
          <span>{selectionValue}</span>
          {selectedRole ? (
            <span className="text-[11px] text-muted-foreground">
              {copy.pricingCardRole}: {selectedRole}
            </span>
          ) : null}
          {selection.kind === "selected" &&
          selection.threshold !== null &&
          selection.basis !== null ? (
            <span className="text-[11px] text-muted-foreground">
              {copy.pricingSelectorThreshold}: <span className="font-mono">{formatNumber(selection.threshold)}</span>
              {" · "}
              {copy.pricingSelectorBasis}: <span className="font-mono">{formatNumber(selection.basis)}</span>
            </span>
          ) : null}
        </div>
      </DetailRow>
      {pricing.pricing_resolution_kind ? (
        <DetailRow label={copy.pricingResolutionKind}>
          {resolutionLabel(pricing.pricing_resolution_kind, copy)}
        </DetailRow>
      ) : null}
      {pricing.pricing_schedule_timezone ? (
        <DetailRow label={copy.pricingScheduleTimezone}>
          <span className="font-mono">{pricing.pricing_schedule_timezone}</span>
        </DetailRow>
      ) : null}
      {pricing.pricing_schedule_decided_at ? (
        <DetailRow label={copy.pricingScheduleDecidedAt}>
          <span className="font-mono">{formatTimestamp(pricing.pricing_schedule_decided_at)}</span>
        </DetailRow>
      ) : null}
      {pricing.pricing_schedule_local_weekday !== null &&
      pricing.pricing_schedule_local_minute !== null ? (
        <DetailRow label={copy.pricingScheduleLocalTime}>
          <span className="font-mono">
            {pricing.pricing_schedule_local_weekday} · {String(Math.floor(pricing.pricing_schedule_local_minute / 60)).padStart(2, "0")}:{String(pricing.pricing_schedule_local_minute % 60).padStart(2, "0")}
          </span>
        </DetailRow>
      ) : null}
      {pricing.pricing_schedule_digest ? (
        <DetailRow label={copy.pricingScheduleDigest}>
          <span className="break-all font-mono text-[11px]">{pricing.pricing_schedule_digest}</span>
        </DetailRow>
      ) : null}
      <DetailRow label={copy.pricingSnapshotInput}><span className="font-mono">{snapshotValue(pricing.pricing_snapshot_input)}</span></DetailRow>
      <DetailRow label={copy.pricingSnapshotOutput}><span className="font-mono">{snapshotValue(pricing.pricing_snapshot_output)}</span></DetailRow>
      <DetailRow label={copy.pricingSnapshotCacheRead}><span className="font-mono">{snapshotValue(pricing.pricing_snapshot_cache_read_input)}</span></DetailRow>
      <DetailRow label={copy.pricingSnapshotCacheCreation}><span className="font-mono">{snapshotValue(pricing.pricing_snapshot_cache_creation_input)}</span></DetailRow>
      <DetailRow label={copy.pricingSnapshotReasoning}><span className="font-mono">{snapshotValue(pricing.pricing_snapshot_reasoning)}</span></DetailRow>
    </>
  );
}
