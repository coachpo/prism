import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { FilterOptions } from "./useRequestLogsPageData";
import type { RequestLogPageActions } from "./useRequestLogPageState";
import { STATUS_FAMILY_OPTIONS, TIME_RANGE_OPTIONS } from "./queryParams";
import { getTimeLabel } from "./FiltersBar.constants";

interface FiltersBarPrimaryFiltersProps {
  actions: Pick<
    RequestLogPageActions,
    | "setIngressRequestId"
    | "setRequestId"
    | "setEndpointId"
    | "setModelId"
    | "setStatusFamily"
    | "setTimeRange"
  >;
  filterOptions: FilterOptions;
  filterOptionsLoaded: boolean;
  state: Pick<
    RequestLogPageActions["state"],
    | "ingress_request_id"
    | "endpoint_id"
    | "model_id"
    | "status_family"
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

  const commitRequestLookup = () => {
    const normalized = requestLookupValue.trim();
    if (!normalized) {
      return;
    }

    actions.setRequestId(normalized);
  };

  return (
    <div className="grid gap-3 xl:grid-cols-6">
      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.requestId}</ToolbarLabel>
        <Input
          name="request_id_lookup"
          autoComplete="off"
          className="h-9 rounded-lg border-border/70 bg-background/80 text-sm"
          placeholder={messages.requestLogs.requestId}
          value={requestLookupValue}
          onChange={(event) => setRequestLookupValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commitRequestLookup();
            }
          }}
        />
      </div>

      <div className="min-w-0 xl:col-span-2">
        <ToolbarLabel>{messages.requestLogs.ingressRequestId}</ToolbarLabel>
        <Input
          name="ingress_request_id"
          autoComplete="off"
          className="h-9 rounded-lg border-border/70 bg-background/80 text-sm font-mono"
          placeholder={messages.requestLogs.ingressRequestId}
          value={state.ingress_request_id}
          onChange={(event) => actions.setIngressRequestId(event.target.value)}
        />
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.model}</ToolbarLabel>
        <Select
          value={state.model_id || "__all__"}
          onValueChange={(value) => actions.setModelId(value === "__all__" ? "" : value)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border/70 bg-background/80 text-xs">
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allModels} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allModels}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.models.map((model) => (
                <SelectItem key={model.model_id} value={model.model_id}>
                  {model.display_name || model.model_id}
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
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border/70 bg-background/80 text-xs">
            <SelectValue className="min-w-0" placeholder={messages.requestLogs.allEndpoints} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{messages.requestLogs.allEndpoints}</SelectItem>
            {filterOptionsLoaded &&
              filterOptions.endpoints.map((endpoint) => (
                <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                  {endpoint.name || endpoint.base_url}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.status}</ToolbarLabel>
        <Select
          value={state.status_family}
          onValueChange={(value) => actions.setStatusFamily(value as typeof state.status_family)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border/70 bg-background/80 text-xs">
            <SelectValue className="min-w-0" />
          </SelectTrigger>
          <SelectContent>
            {STATUS_FAMILY_OPTIONS.map((statusFamily) => (
              <SelectItem key={statusFamily} value={statusFamily}>
                {statusFamily === "all"
                  ? messages.requestLogs.allStatuses
                  : statusFamily === "4xx"
                    ? messages.requestLogs.fourHundredsOnly
                    : messages.requestLogs.fiveHundredsOnly}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="min-w-0">
        <ToolbarLabel>{messages.requestLogs.timeRange}</ToolbarLabel>
        <Select
          value={state.time_range}
          onValueChange={(value) => actions.setTimeRange(value as typeof state.time_range)}
        >
          <SelectTrigger className="h-9 w-full min-w-0 max-w-full rounded-lg border-border/70 bg-background/80 text-xs">
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
  );
}
