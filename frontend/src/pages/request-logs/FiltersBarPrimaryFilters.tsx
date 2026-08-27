import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { RequestLogFilterOptions as FilterOptions } from "./requestLogQuery";
import type { RequestLogPageActions } from "./useRequestLogPageState";
import { PRICING_CARD_ROLE_OPTIONS, PRICING_SELECTION_STATE_OPTIONS, PRICING_STATUS_OPTIONS, STATUS_FAMILY_OPTIONS, TIME_RANGE_OPTIONS, UNPRICED_REASON_OPTIONS } from "./queryParams";
import { getTimeLabel, getUnpricedReasonLabel } from "./FiltersBar.constants";
import { useRequestLogProxyApiKeyOptions } from "./useRequestLogProxyApiKeyOptions";


interface FiltersBarPrimaryFiltersProps {
  actions: Pick<
    RequestLogPageActions,
    | "setIngressRequestId"
    | "setRequestId"
    | "setEndpointId"
    | "setTerminalTargetId"
    | "setModelId"
    | "setClientRuleId"
    | "setProxyApiKeyId"
    | "setResolvedTargetModelId"
    | "setStatusFamily"
    | "setStatusCode"
    | "setErrorText"
    | "setPricingStatus"
    | "setUnpricedReason"
    | "setPricingCardRole"
    | "setPricingSelectionState"
    | "setTimeRange"
  >;
  filterOptions: FilterOptions;
  filterOptionsLoaded: boolean;
  state: Pick<
    RequestLogPageActions["state"],
    | "ingress_request_id"
    | "endpoint_id"
    | "terminal_target_id"
    | "model_id"
    | "client_rule_id"
    | "proxy_api_key_id"
    | "resolved_target_model_id"
    | "status_family"
    | "status_code"
    | "error_text"
    | "pricing_status"
    | "unpriced_reason"
    | "pricing_card_role"
    | "pricing_selection_state"
    | "time_range"
  >;
}

function ToolbarLabel({ children }: { children: React.ReactNode }) {
  return (
    <Label className="mb-1.5 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
      {children}
    </Label>
  );
}

export function FiltersBarPrimaryFilters({
  actions,
  filterOptions,
  filterOptionsLoaded,
  state,
}: FiltersBarPrimaryFiltersProps) {
  const { messages } = useLocale();
  const [requestLookupValue, setRequestLookupValue] = useState("");
  const [moreOpen, setMoreOpen] = useState(false);
  const {
    options: proxyKeyOptions,
    search: proxyKeySearch,
    setSearch: setProxyKeySearch,
  } = useRequestLogProxyApiKeyOptions(state.proxy_api_key_id);

  // Conditions that live inside the collapsed panel.
  const hiddenFilterCount = [
    state.model_id,
    state.resolved_target_model_id,
    state.endpoint_id,
    state.terminal_target_id,
    state.proxy_api_key_id,
    state.client_rule_id,
    state.status_code,
    state.error_text,
    state.unpriced_reason,
    state.pricing_card_role,
    state.pricing_selection_state,
    state.status_family !== "all" ? state.status_family : "",
    state.pricing_status !== "all" ? state.pricing_status : "",
  ].filter(Boolean).length;

  const commitRequestLookup = () => {
    const normalized = requestLookupValue.trim();
    if (!normalized) {
      return;
    }

    actions.setRequestId(normalized);
  };

  // Compact layout (Requests SPEC §10.5/AC 20): the visible row stays to
  // request ID search + time range + the More Filters toggle; the remaining
  // filters collapse under More Filters instead of a permanent 12-control
  // grid.
  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 xl:grid-cols-12">
      {/* Explicit submit: typing an id no longer silently swaps the page into
          a different mode, and the filters stay adjustable afterwards. */}
      <div className="min-w-0 xl:col-span-3">
        <ToolbarLabel>{messages.requestLogs.requestId}</ToolbarLabel>
        <div className="flex items-center gap-1">
          <Input
            name="request_id_lookup"
            autoComplete="off"
            className="h-9 rounded-md border-border bg-panel text-sm"
            placeholder={messages.requestLogs.locateRequestPlaceholder}
            value={requestLookupValue}
            onChange={(event) => setRequestLookupValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                commitRequestLookup();
              }
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-9 shrink-0"
            disabled={!requestLookupValue.trim()}
            onClick={commitRequestLookup}
            data-testid="request-logs-locate"
          >
            {messages.requestLogs.locateRequest}
          </Button>
        </div>
      </div>

      <div className="min-w-0 xl:col-span-2">
        <ToolbarLabel>{messages.requestLogs.ingressRequestId}</ToolbarLabel>
        <Input
          name="ingress_request_id"
          autoComplete="off"
          className="h-9 rounded-lg border-border bg-panel text-sm font-mono"
          placeholder={messages.requestLogs.ingressRequestId}
          value={state.ingress_request_id}
          onChange={(event) => actions.setIngressRequestId(event.target.value)}
        />
      </div>

      </div>
      <div className="flex flex-col gap-2 rounded-lg border border-border bg-inset/50 p-3">
        {/* The count makes conditions hidden behind the collapse visible even
            when the panel is shut, so a deep link cannot filter silently. */}
        <button
          type="button"
          aria-expanded={moreOpen}
          className="inline-flex items-center gap-1.5 self-start rounded-md border border-border bg-panel px-2.5 py-1 text-xs font-medium text-muted-foreground hover:bg-inset"
          onClick={() => setMoreOpen((open) => !open)}
        >
          <ChevronDown className={cn("size-3.5 transition-transform", moreOpen && "rotate-180")} />
          {messages.requestLogs.moreFilters ?? "更多筛选"}
          {hiddenFilterCount > 0 ? (
            <span
              data-testid="more-filters-count"
              className="inline-flex h-4 min-w-4 items-center justify-center rounded-[4px] bg-primary px-1 font-mono text-[10px] tabular-nums text-on-primary"
            >
              {hiddenFilterCount}
            </span>
          ) : null}
        </button>
        {moreOpen ? (
          <div className="grid gap-3 xl:grid-cols-12">
      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.entryModel}</ToolbarLabel>
        <Select
          value={state.model_id || "__all__"}
          onValueChange={(value) => actions.setModelId(value === "__all__" ? "" : value)}
        >
          <SelectTrigger
            aria-label={messages.requestLogs.entryModel}
            className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs"
          >
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allModels} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allModels}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.models.map((model) => (
                <SelectItem key={model.ingress_model_id} value={model.ingress_model_id}>
                  {model.model_label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.client}</ToolbarLabel>
        <Select
          value={state.client_rule_id || "__all__"}
          onValueChange={(value) => actions.setClientRuleId(value === "__all__" ? "" : value)}
        >
          <SelectTrigger
            aria-label={messages.requestLogs.client}
            className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs"
          >
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allClients} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allClients}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.clients.map((client) => (
                <SelectItem key={client.client_rule_id} value={String(client.client_rule_id)}>
                  {client.client_label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0 xl:col-span-2">
        <ToolbarLabel>{messages.requestLogs.proxyKey}</ToolbarLabel>
        <div className="flex items-center gap-1">
          <Input
            name="proxy_api_key_search"
            autoComplete="off"
            className="h-9 w-28 rounded-lg border-border bg-panel text-xs"
            placeholder={messages.requestLogs.proxyKeySearch}
            value={proxyKeySearch}
            onChange={(event) => setProxyKeySearch(event.target.value)}
          />
          <Select
            value={state.proxy_api_key_id || "__all__"}
            onValueChange={(value) => actions.setProxyApiKeyId(value === "__all__" ? "" : value)}
          >
            <SelectTrigger aria-label={messages.requestLogs.proxyKey} className="h-9 w-full min-w-0 rounded-lg border-border bg-panel text-xs">
              <SelectValue placeholder={messages.requestLogs.allProxyKeys} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{messages.requestLogs.allProxyKeys}</SelectItem>
              {proxyKeyOptions.map((option) => (
                <SelectItem key={option.proxy_api_key_id} value={String(option.proxy_api_key_id)}>
                  {option.proxy_api_key_name}
                  {option.key_preview ? ` · ${option.key_preview}` : ""}
                  {!option.configured ? ` · ${messages.requestLogs.proxyKeyDeleted}` : ""}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.attemptTargetModel}</ToolbarLabel>
        <Select
          value={state.resolved_target_model_id || "__all__"}
          onValueChange={(value) => actions.setResolvedTargetModelId(value === "__all__" ? "" : value)}
        >
          <SelectTrigger
            aria-label={messages.requestLogs.attemptTargetModel}
            className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs"
          >
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allAttemptTargetModels} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allAttemptTargetModels}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.resolved_target_models.map((model) => (
                <SelectItem key={model.attempt_target_model_id} value={model.attempt_target_model_id}>
                  {model.model_label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.endpoint}</ToolbarLabel>
        <Select
          value={state.endpoint_id || "__all__"}
          onValueChange={(value) => actions.setEndpointId(value === "__all__" ? "" : value)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs">
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allEndpoints} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allEndpoints}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.endpoints.map((endpoint) => (
                <SelectItem key={endpoint.endpoint_id} value={String(endpoint.endpoint_id)}>
                  {endpoint.endpoint_label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.terminalTarget ?? "Terminal Target"}</ToolbarLabel>
        <Input
          name="terminal_target_id"
          autoComplete="off"
          inputMode="numeric"
          className="h-9 rounded-lg border-border bg-panel text-sm font-mono"
          placeholder="#"
          value={state.terminal_target_id}
          onChange={(event) => actions.setTerminalTargetId(event.target.value)}
        />
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.status}</ToolbarLabel>
        <Select
          value={state.status_family}
          onValueChange={(value) => actions.setStatusFamily(value as typeof state.status_family)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs">
            <SelectValue className="min-w-0" />
          </SelectTrigger>
          <SelectContent>
            {STATUS_FAMILY_OPTIONS.map((statusFamily) => (
              <SelectItem key={statusFamily} value={statusFamily}>
                {statusFamily === "all"
                  ? messages.requestLogs.allStatuses
                  : statusFamily === "2xx"
                    ? messages.requestLogs.twoHundredsOnly
                    : statusFamily === "4xx"
                    ? messages.requestLogs.fourHundredsOnly
                    : messages.requestLogs.fiveHundredsOnly}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.pricedFilterLabel}</ToolbarLabel>
        <Select
          value={state.pricing_status}
          onValueChange={(value) => actions.setPricingStatus(value as typeof state.pricing_status)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs">
            <SelectValue className="min-w-0" />
          </SelectTrigger>
          <SelectContent>
            {PRICING_STATUS_OPTIONS.map((pricingStatus) => (
              <SelectItem key={pricingStatus} value={pricingStatus}>
                {pricingStatus === "all"
                  ? messages.requestLogs.any
                  : pricingStatus === "priced"
                    ? messages.requestLogs.pricedOnly
                    : pricingStatus === "unpriced"
                      ? messages.requestLogs.unpricedOnly
                      : pricingStatus === "ineligible"
                        ? (messages.requestLogs.ineligible ?? "不适用")
                        : (messages.requestLogs.unknown ?? "未知")}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0 xl:col-span-2">
        <ToolbarLabel>{messages.requestLogs.unpricedReasonLabel}</ToolbarLabel>
        <Select
          value={state.unpriced_reason || "__all__"}
          onValueChange={(value) => actions.setUnpricedReason(value === "__all__" ? "" : value)}
          disabled={state.pricing_status !== "unpriced"}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs">
            <SelectValue className="min-w-0" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.any}</SelectItem>
            {UNPRICED_REASON_OPTIONS.map((reason) => (
              <SelectItem key={reason} value={reason}>
                {getUnpricedReasonLabel(reason)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.pricingCardRole}</ToolbarLabel>
        <Select value={state.pricing_card_role || "__all__"} onValueChange={(value) => actions.setPricingCardRole(value === "__all__" ? "" : value as typeof state.pricing_card_role)}>
          <SelectTrigger className="h-9 w-full rounded-lg border-border bg-panel text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allPricingCardRoles}</SelectItem>
            {PRICING_CARD_ROLE_OPTIONS.map((role) => <SelectItem key={role} value={role}>{role === "standard" ? messages.requestLogs.pricingCardStandard : role === "tier_base" ? messages.requestLogs.pricingCardTierBase : role === "tier_above" ? messages.requestLogs.pricingCardTierAbove : role === "peak" ? messages.requestLogs.pricingCardPeak : messages.requestLogs.pricingCardOffpeak}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.pricingSelectionState}</ToolbarLabel>
        <Select value={state.pricing_selection_state || "__all__"} onValueChange={(value) => actions.setPricingSelectionState(value === "__all__" ? "" : value as typeof state.pricing_selection_state)}>
          <SelectTrigger className="h-9 w-full rounded-lg border-border bg-panel text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allPricingSelectionStates}</SelectItem>
            {PRICING_SELECTION_STATE_OPTIONS.map((selection) => <SelectItem key={selection} value={selection}>{selection === "not_evaluated" ? messages.requestLogs.pricingSelectionNotEvaluated : selection === "not_applicable" ? messages.requestLogs.pricingSelectionNotApplicable : selection === "selected" ? messages.requestLogs.pricingSelectionSelected : messages.requestLogs.pricingSelectionUnresolved}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.statusCodeFilterLabel}</ToolbarLabel>
        <Input
          name="status_code"
          autoComplete="off"
          inputMode="numeric"
          pattern="[0-9]*"
          className="h-9 rounded-lg border-border bg-panel text-sm font-mono"
          placeholder="429"
          value={state.status_code}
          onChange={(event) => actions.setStatusCode(event.target.value)}
        />
      </div>

      <div className="min-w-0 xl:col-span-2">
        <ToolbarLabel>{messages.requestLogs.errorTextFilterLabel}</ToolbarLabel>
        <Input
          name="error_text"
          autoComplete="off"
          className="h-9 rounded-lg border-border bg-panel text-sm"
          placeholder={messages.requestLogs.errorDetail}
          value={state.error_text}
          onChange={(event) => actions.setErrorText(event.target.value)}
        />
      </div>

          </div>
        ) : null}
      </div>
      <div className="grid gap-3 xl:grid-cols-12">
      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.timeRange}</ToolbarLabel>
        <Select
          value={state.time_range}
          onValueChange={(value) => actions.setTimeRange(value as typeof state.time_range)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border bg-panel text-xs">
            <SelectValue className="min-w-0" />
          </SelectTrigger>
          <SelectContent>
            {TIME_RANGE_OPTIONS.map((timeRange) => (
              <SelectItem key={timeRange} value={timeRange}>
                {getTimeLabel(timeRange)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      </div>
    </div>
  );
}
