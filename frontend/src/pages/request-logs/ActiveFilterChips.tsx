import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { applyRequestLogStatePatch } from "./queryParams";
import type { useRequestLogPageState } from "./useRequestLogPageState";

type Actions = ReturnType<typeof useRequestLogPageState>;

type Chip = {
  key: string;
  label: string;
  onClear: () => void;
  value: string;
};

/**
 * Every filter actually in effect, as a closable chip.
 *
 * A deep link can arrive carrying half a dozen conditions that live behind a
 * collapsed "more filters" panel. Without this row the list looks like it is
 * showing everything while it is showing a slice, which is the same class of
 * mistake as painting a partial result as a complete one.
 */
export function ActiveFilterChips({ actions }: { actions: Actions }) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;
  const { state } = actions;

  const chips: Chip[] = [];
  const push = (key: string, label: string, value: string | null | undefined, onClear: () => void) => {
    if (!value) return;
    chips.push({ key, label, onClear, value });
  };

  push("ingress_request_id", copy.ingressRequestId, state.ingress_request_id, () => actions.setIngressRequestId(""));
  push("ingress_model_id", copy.entryModel, state.model_id, () => actions.setModelId(""));
  push("attempt_target_model_id", copy.attemptTargetModel, state.resolved_target_model_id, () => actions.setResolvedTargetModelId(""));
  push("final_target_model_id", copy.finalTargetModel, state.final_target_model_id, () =>
    actions.replaceState(
      applyRequestLogStatePatch(state, { final_target_model_id: "" }),
    ),
  );
  push("final_result", copy.finalResult, state.final_result, () =>
    actions.replaceState(
      applyRequestLogStatePatch(state, { final_result: "" }),
    ),
  );
  push("endpoint_id", copy.endpoint, state.endpoint_id, () => actions.setEndpointId(""));
  push("terminal_target_id", copy.terminalTarget, state.terminal_target_id, () => actions.setTerminalTargetId(""));
  push("proxy_api_key_id", copy.proxyApiKey, state.proxy_api_key_id, () => actions.setProxyApiKeyId(""));
  push("client_rule_id", copy.clientRule, state.client_rule_id, () => actions.setClientRuleId(""));
  push("status_code", copy.statusCode, state.status_code, () => actions.setStatusCode(""));
  push("error_text", copy.errorText, state.error_text, () => actions.setErrorText(""));
  push("unpriced_reason", copy.unpricedReason, state.unpriced_reason, () => actions.setUnpricedReason(""));
  push("pricing_card_role", copy.pricingCardRole, state.pricing_card_role, () => actions.setPricingCardRole(""));
  push("pricing_selection_state", copy.pricingSelectionState, state.pricing_selection_state, () => actions.setPricingSelectionState(""));

  if (state.status_family !== "all") {
    push("status_family", copy.statusFamily, state.status_family, () => actions.setStatusFamily("all"));
  }
  if (state.pricing_status !== "all") {
    push("pricing_status", copy.pricedStatus, state.pricing_status, () => actions.setPricingStatus("all"));
  }
  if (state.ingress_final_result) {
    push("ingress_final_result", copy.finalResult, state.ingress_final_result, () => actions.setIngressFinalResult(""));
  }
  if (state.confirmed_failover) {
    push("confirmed_failover", copy.confirmedFailoverChip, copy.chipOn, () => actions.setConfirmedFailover(false));
  }
  if (state.time_range !== "24h") {
    push("time_range", copy.timeRange, state.time_range, () => actions.setTimeRange("24h"));
  }

  if (chips.length === 0) return null;

  return (
    <div
      data-testid="active-filter-chips"
      className="flex flex-wrap items-center gap-1.5"
      aria-label={copy.activeFiltersLabel}
    >
      <span className="text-[11px] font-medium tracking-[0.04em] text-muted-foreground">
        {copy.activeFiltersLabel}
      </span>
      {chips.map((chip) => (
        <span
          key={chip.key}
          data-testid={`filter-chip-${chip.key}`}
          className="inline-flex h-5 items-center gap-1 rounded-[4px] border border-primary/25 bg-primary/10 pl-1.5 pr-0.5 text-xs text-primary"
        >
          <span className="text-muted-foreground">{chip.label}</span>
          <span className="max-w-40 truncate font-mono">{chip.value}</span>
          <button
            type="button"
            onClick={chip.onClear}
            aria-label={copy.removeFilterAria(chip.label)}
            className="flex size-4 items-center justify-center rounded-[3px] hover:bg-primary/20"
          >
            <X aria-hidden="true" className="size-3" />
          </button>
        </span>
      ))}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-5 px-1.5 text-xs"
        onClick={actions.clearFilters}
      >
        {messages.statistics.clearFilters}
      </Button>
    </div>
  );
}
