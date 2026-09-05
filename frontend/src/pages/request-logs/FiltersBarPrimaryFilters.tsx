import { useId, useState, type ReactNode } from "react";
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
  /** 折叠面板由筛选卡的操作行控制，展开态由上层持有。 */
  moreOpen: boolean;
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
    | "request_id"
  >;
}

interface FilterFieldIds {
  /** 控件本身的 id，可见标签的 htmlFor 指向它。 */
  controlId: string;
  /** 可见标签的 id，下拉用 aria-labelledby 指回来。 */
  labelId: string;
}

/**
 * 一个筛选字段 = 一个可见标签 + 一个被它命名的控件。
 *
 * 标签与控件必须真的关联：placeholder 不是标签（一输入就消失），下拉在没有
 * 可访问名时读屏听到的是当前取值而不是字段名，两个「任意」根本分不出来。
 */
function FilterField({
  children,
  className,
  label,
}: {
  children: (ids: FilterFieldIds) => ReactNode;
  className?: string;
  label: string;
}) {
  const id = useId();
  const ids: FilterFieldIds = {
    controlId: `${id}-control`,
    labelId: `${id}-label`,
  };
  return (
    <div className={cn("min-w-0", className)}>
      <Label
        id={ids.labelId}
        htmlFor={ids.controlId}
        className="mb-1.5 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground"
      >
        {label}
      </Label>
      {children(ids)}
    </div>
  );
}

// 下拉一律给一个按最长选项算出来的下限：没有下限时 12 栏栅格在 1440 上把
// 「最近 24 小时」压到 19px，操作者既读不出当前口径也看不出选项差异。
const SELECT_TRIGGER_CLASS =
  "h-9 w-full max-w-full rounded-lg border-border bg-panel text-xs";

export function FiltersBarPrimaryFilters({
  actions,
  filterOptions,
  filterOptionsLoaded,
  moreOpen,
  state,
}: FiltersBarPrimaryFiltersProps) {
  const { messages } = useLocale();
  // URL 里带着 request_id 时输入框却是空的，操作者看不出是什么条件造成了
  // 当前这一屏。定位值以 URL 为准，回显进来。
  const [requestLookupValue, setRequestLookupValue] = useState(state.request_id);
  const [echoedRequestId, setEchoedRequestId] = useState(state.request_id);
  if (echoedRequestId !== state.request_id) {
    setEchoedRequestId(state.request_id);
    setRequestLookupValue(state.request_id);
  }
  const {
    options: proxyKeyOptions,
    search: proxyKeySearch,
    setSearch: setProxyKeySearch,
  } = useRequestLogProxyApiKeyOptions(state.proxy_api_key_id);

  const commitRequestLookup = () => {
    const normalized = requestLookupValue.trim();
    if (!normalized) {
      return;
    }

    actions.setRequestId(normalized);
  };

  // Compact layout (Requests SPEC §10.5/AC 20): the visible row stays to
  // request ID search + time range; the remaining filters collapse under the
  // More Filters toggle in the card's action row instead of a permanent
  // 12-control grid.
  return (
    <div className="flex flex-col gap-2">
      {/* 弹性行而不是 12 栏栅格：<1280 时栅格塌成单列，一个下拉独占整行；
          栅格轨道又是 minmax(0,1fr)，把控件压到读不出当前取值。 */}
      <div className="flex flex-wrap items-end gap-3">
        {/* Explicit submit: typing an id no longer silently swaps the page into
            a different mode, and the filters stay adjustable afterwards. */}
        <FilterField
          label={messages.requestLogs.requestId}
          className="min-w-[17rem] flex-[2]"
        >
          {({ controlId }) => (
            <div className="flex items-center gap-1">
              <Input
                id={controlId}
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
          )}
        </FilterField>

        <FilterField
          label={messages.requestLogs.ingressRequestId}
          className="min-w-[13rem] flex-1"
        >
          {({ controlId }) => (
            <Input
              id={controlId}
              name="ingress_request_id"
              autoComplete="off"
              className="h-9 rounded-lg border-border bg-panel font-mono text-sm"
              placeholder={messages.requestLogs.ingressRequestId}
              value={state.ingress_request_id}
              onChange={(event) => actions.setIngressRequestId(event.target.value)}
            />
          )}
        </FilterField>

        {/* 时间范围是这一屏的口径，不是众多筛选之一：它留在常显行里，
            默认 24h 落地空表时操作者一眼能找到唯一的解法。 */}
        <FilterField
          label={messages.requestLogs.timeRange}
          className="w-[10rem] shrink-0"
        >
          {({ controlId, labelId }) => (
            <Select
              value={state.time_range}
              onValueChange={(value) => actions.setTimeRange(value as typeof state.time_range)}
            >
              <SelectTrigger
                id={controlId}
                aria-labelledby={`${labelId} ${controlId}`}
                className={SELECT_TRIGGER_CLASS}
                data-testid="request-logs-time-range"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TIME_RANGE_OPTIONS.map((timeRange) => (
                  <SelectItem key={timeRange} value={timeRange}>
                    {getTimeLabel(timeRange)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </FilterField>
      </div>

      {moreOpen ? (
        // auto-fit 轨道给每个控件一个 9.5rem 下限，宽屏放更多列、窄屏自动换行：
        // 屏幕越宽控件越窄的方向反了的问题在这里一次解决。
        <div
          id="request-logs-more-filters"
          data-testid="request-logs-more-filters"
          className="grid gap-3 rounded-lg border border-border bg-inset/50 p-3 grid-cols-[repeat(auto-fit,minmax(9.5rem,1fr))]"
        >
          <FilterField label={messages.requestLogs.entryModel}>
            {({ controlId, labelId }) => (
              <Select
                value={state.model_id || "__all__"}
                onValueChange={(value) => actions.setModelId(value === "__all__" ? "" : value)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue placeholder={messages.requestLogs.allModels} />
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.client}>
            {({ controlId, labelId }) => (
              <Select
                value={state.client_rule_id || "__all__"}
                onValueChange={(value) => actions.setClientRuleId(value === "__all__" ? "" : value)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue placeholder={messages.requestLogs.allClients} />
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
            )}
          </FilterField>

          <FilterField
            label={messages.requestLogs.proxyKey}
            className="col-span-full sm:col-span-2"
          >
            {({ controlId, labelId }) => (
              <div className="flex items-center gap-1">
                <Input
                  name="proxy_api_key_search"
                  autoComplete="off"
                  aria-label={messages.requestLogs.proxyKeySearch}
                  className="h-9 w-24 shrink-0 rounded-lg border-border bg-panel text-xs"
                  placeholder={messages.requestLogs.proxyKeySearch}
                  value={proxyKeySearch}
                  onChange={(event) => setProxyKeySearch(event.target.value)}
                />
                <Select
                  value={state.proxy_api_key_id || "__all__"}
                  onValueChange={(value) => actions.setProxyApiKeyId(value === "__all__" ? "" : value)}
                >
                  <SelectTrigger
                    id={controlId}
                    aria-labelledby={`${labelId} ${controlId}`}
                    className={SELECT_TRIGGER_CLASS}
                  >
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.attemptTargetModel}>
            {({ controlId, labelId }) => (
              <Select
                value={state.resolved_target_model_id || "__all__"}
                onValueChange={(value) => actions.setResolvedTargetModelId(value === "__all__" ? "" : value)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue placeholder={messages.requestLogs.allAttemptTargetModels} />
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.endpoint}>
            {({ controlId, labelId }) => (
              <Select
                value={state.endpoint_id || "__all__"}
                onValueChange={(value) => actions.setEndpointId(value === "__all__" ? "" : value)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue placeholder={messages.requestLogs.allEndpoints} />
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.terminalTarget}>
            {({ controlId }) => (
              <Input
                id={controlId}
                name="terminal_target_id"
                autoComplete="off"
                inputMode="numeric"
                className="h-9 rounded-lg border-border bg-panel font-mono text-sm"
                placeholder="#"
                value={state.terminal_target_id}
                onChange={(event) => actions.setTerminalTargetId(event.target.value)}
              />
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.status}>
            {({ controlId, labelId }) => (
              <Select
                value={state.status_family}
                onValueChange={(value) => actions.setStatusFamily(value as typeof state.status_family)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue />
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.pricedFilterLabel}>
            {({ controlId, labelId }) => (
              <Select
                value={state.pricing_status}
                onValueChange={(value) => actions.setPricingStatus(value as typeof state.pricing_status)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue />
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
                              ? messages.requestLogs.ineligible
                              : messages.requestLogs.unknown}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </FilterField>

          <FilterField
            label={messages.requestLogs.unpricedReasonLabel}
            className="sm:col-span-2"
          >
            {({ controlId, labelId }) => (
              <Select
                value={state.unpriced_reason || "__all__"}
                onValueChange={(value) => actions.setUnpricedReason(value === "__all__" ? "" : value)}
                disabled={state.pricing_status !== "unpriced"}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue />
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
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.pricingCardRole}>
            {({ controlId, labelId }) => (
              <Select
                value={state.pricing_card_role || "__all__"}
                onValueChange={(value) => actions.setPricingCardRole(value === "__all__" ? "" : value as typeof state.pricing_card_role)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__all__">{messages.requestLogs.allPricingCardRoles}</SelectItem>
                  {PRICING_CARD_ROLE_OPTIONS.map((role) => (
                    <SelectItem key={role} value={role}>
                      {role === "standard"
                        ? messages.requestLogs.pricingCardStandard
                        : role === "tier_base"
                          ? messages.requestLogs.pricingCardTierBase
                          : role === "tier_above"
                            ? messages.requestLogs.pricingCardTierAbove
                            : role === "peak"
                              ? messages.requestLogs.pricingCardPeak
                              : messages.requestLogs.pricingCardOffpeak}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.pricingSelectionState}>
            {({ controlId, labelId }) => (
              <Select
                value={state.pricing_selection_state || "__all__"}
                onValueChange={(value) => actions.setPricingSelectionState(value === "__all__" ? "" : value as typeof state.pricing_selection_state)}
              >
                <SelectTrigger
                  id={controlId}
                  aria-labelledby={`${labelId} ${controlId}`}
                  className={SELECT_TRIGGER_CLASS}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__all__">{messages.requestLogs.allPricingSelectionStates}</SelectItem>
                  {PRICING_SELECTION_STATE_OPTIONS.map((selection) => (
                    <SelectItem key={selection} value={selection}>
                      {selection === "not_evaluated"
                        ? messages.requestLogs.pricingSelectionNotEvaluated
                        : selection === "not_applicable"
                          ? messages.requestLogs.pricingSelectionNotApplicable
                          : selection === "selected"
                            ? messages.requestLogs.pricingSelectionSelected
                            : messages.requestLogs.pricingSelectionUnresolved}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </FilterField>

          <FilterField label={messages.requestLogs.statusCodeFilterLabel}>
            {({ controlId }) => (
              <Input
                id={controlId}
                name="status_code"
                autoComplete="off"
                inputMode="numeric"
                pattern="[0-9]*"
                className="h-9 rounded-lg border-border bg-panel font-mono text-sm"
                placeholder="429"
                value={state.status_code}
                onChange={(event) => actions.setStatusCode(event.target.value)}
              />
            )}
          </FilterField>

          <FilterField
            label={messages.requestLogs.errorTextFilterLabel}
            className="sm:col-span-2"
          >
            {({ controlId }) => (
              <Input
                id={controlId}
                name="error_text"
                autoComplete="off"
                className="h-9 rounded-lg border-border bg-panel text-sm"
                placeholder={messages.requestLogs.errorDetail}
                value={state.error_text}
                onChange={(event) => actions.setErrorText(event.target.value)}
              />
            )}
          </FilterField>
        </div>
      ) : null}
    </div>
  );
}
