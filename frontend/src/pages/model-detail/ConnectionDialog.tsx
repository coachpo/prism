import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import { SwitchController } from "@/components/SwitchController";
import { StatusBadge } from "@/components/StatusBadge";
import { useLocale } from "@/i18n/useLocale";
import { Loader2, Plus, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { createHeaderRow } from "./useModelDetailDialogState";
import type {
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  PricingTemplate,
} from "@/lib/types";
import type { HeaderRow } from "./useModelDetailDialogState";

interface ConnectionDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  editingConnection: Connection | null;
  connectionForm: ConnectionCreate;
  setConnectionForm: (form: ConnectionCreate) => void;
  newEndpointForm: EndpointCreate;
  setNewEndpointForm: (form: EndpointCreate) => void;
  createMode: "select" | "new";
  setCreateMode: (mode: "select" | "new") => void;
  selectedEndpointId: string;
  setSelectedEndpointId: (id: string) => void;
  globalEndpoints: Endpoint[];
  headerRows: HeaderRow[];
  setHeaderRows: (rows: HeaderRow[]) => void;
  handleConnectionSubmit: (e: React.FormEvent) => Promise<void>;
  dialogTestingConnection: boolean;
  dialogTestResult: { status: string; detail: string } | null;
  handleDialogTestConnection: () => Promise<void>;
  endpointSourceDefaultName: string | null;
  pricingTemplates: PricingTemplate[];
}

export function ConnectionDialog({
  isOpen,
  onOpenChange,
  editingConnection,
  connectionForm,
  setConnectionForm,
  newEndpointForm,
  setNewEndpointForm,
  createMode,
  setCreateMode,
  selectedEndpointId,
  setSelectedEndpointId,
  globalEndpoints,
  headerRows,
  setHeaderRows,
  handleConnectionSubmit,
  dialogTestingConnection,
  dialogTestResult,
  handleDialogTestConnection,
  endpointSourceDefaultName,
  pricingTemplates,
}: ConnectionDialogProps) {
  const selectedEndpoint = globalEndpoints.find(
    (endpoint) => endpoint.id === Number.parseInt(selectedEndpointId, 10)
  );
  const { messages } = useLocale();
  const copy = messages.modelDetail;

  const limiterFields: Array<{
    field: "qps_limit" | "max_in_flight_non_stream" | "max_in_flight_stream";
    id: string;
    label: string;
    value: number | null | undefined;
  }> = [
    {
      field: "qps_limit",
      id: "conn-qps-limit",
      label: copy.qpsLimit,
      value: connectionForm.qps_limit,
    },
    {
      field: "max_in_flight_non_stream",
      id: "conn-max-in-flight-non-stream",
      label: copy.maxInFlightNonStream,
      value: connectionForm.max_in_flight_non_stream,
    },
    {
      field: "max_in_flight_stream",
      id: "conn-max-in-flight-stream",
      label: copy.maxInFlightStream,
      value: connectionForm.max_in_flight_stream,
    },
  ];

  const handleLimiterChange = (
    field: "qps_limit" | "max_in_flight_non_stream" | "max_in_flight_stream",
    rawValue: string,
  ) => {
    const nextValue = rawValue === "" ? null : Number.parseInt(rawValue, 10);
    setConnectionForm({
      ...connectionForm,
      [field]: Number.isNaN(nextValue) ? null : nextValue,
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(94vh,64rem)] max-h-[94vh] max-w-5xl flex-col overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="shrink-0 border-b bg-background px-6 py-5 sm:px-7">
          <DialogTitle>{editingConnection ? copy.editConnection : copy.addConnection}</DialogTitle>
          <DialogDescription>{copy.connectionDialogDescription}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleConnectionSubmit} className="flex min-h-0 flex-1 flex-col">
          <input type="hidden" name="create_mode" value={createMode} />
          <input
            type="hidden"
            name="selected_endpoint_id"
            value={createMode === "select" ? selectedEndpointId : ""}
          />
          <input type="hidden" name="is_active" value={String(connectionForm.is_active ?? true)} />
          <input
            type="hidden"
            name="pricing_template_id"
            value={connectionForm.pricing_template_id === null ? "" : String(connectionForm.pricing_template_id)}
          />
          <ScrollArea className="min-h-0 flex-1">
            <div className="grid gap-6 px-6 py-5 sm:px-7" data-testid="connection-dialog-scroll-body">
              <div
                className="space-y-4 rounded-2xl border bg-muted/20 p-4 sm:p-5"
                data-testid="connection-dialog-endpoint-source-section"
              >
                <div className="flex items-start justify-between gap-2.5">
                  <div>
                    <Label className="text-sm font-medium">{copy.endpointSource}</Label>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {editingConnection ? copy.endpointSourceEditHint : copy.endpointSourceCreateHint}
                    </p>
                  </div>
                  {editingConnection && <StatusBadge label={copy.editable} intent="info" />}
                </div>

                <Tabs
                  value={createMode}
                  onValueChange={(value) => setCreateMode(value as "select" | "new")}
                  className="gap-4"
                >
                  <TabsList className="grid w-full md:max-w-md grid-cols-2">
                    <TabsTrigger value="select">{copy.selectExisting}</TabsTrigger>
                    <TabsTrigger value="new">{copy.createNew}</TabsTrigger>
                  </TabsList>

                  <TabsContent value="select" className="space-y-2.5">
                    <div className="space-y-2">
                      <Label htmlFor="conn-selected-endpoint">{copy.selectEndpoint}</Label>
                      <Select value={selectedEndpointId} onValueChange={setSelectedEndpointId}>
                        <SelectTrigger id="conn-selected-endpoint">
                          <SelectValue placeholder={copy.selectEndpointPlaceholder} />
                        </SelectTrigger>
                        <SelectContent>
                          {globalEndpoints.map((endpoint) => (
                            <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                              {endpoint.name} ({endpoint.base_url})
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    {selectedEndpoint ? (
                      <p className="text-[11px] text-muted-foreground">
                        <span className="font-medium text-foreground">
                          {copy.selectedEndpoint(selectedEndpoint.name)}
                        </span>
                      </p>
                    ) : null}
                    {globalEndpoints.length === 0 ? (
                      <p className="text-xs text-muted-foreground">{copy.noProfileEndpointsFound}</p>
                    ) : null}
                  </TabsContent>

                  <TabsContent value="new" className="grid gap-3 md:grid-cols-2" data-testid="connection-dialog-create-new-grid">
                    <div className="space-y-2">
                      <Label htmlFor="endpoint-name">{copy.endpointName}</Label>
                      <Input
                        id="endpoint-name"
                        name="endpoint_name"
                        autoComplete="off"
                        placeholder={copy.endpointNamePlaceholder}
                        value={newEndpointForm.name}
                        onChange={(e) => setNewEndpointForm({ ...newEndpointForm, name: e.target.value })}
                        required={createMode === "new"}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="endpoint-base-url">{copy.endpointBaseUrl}</Label>
                      <Input
                        id="endpoint-base-url"
                        name="endpoint_base_url"
                        autoComplete="off"
                        placeholder={copy.endpointBaseUrlPlaceholder}
                        value={newEndpointForm.base_url}
                        onChange={(e) => setNewEndpointForm({ ...newEndpointForm, base_url: e.target.value })}
                        required={createMode === "new"}
                      />
                    </div>
                    <div className="space-y-2 md:col-span-2">
                      <Label htmlFor="endpoint-api-key">{copy.endpointApiKey}</Label>
                      <Input
                        id="endpoint-api-key"
                        name="endpoint_api_key"
                        type="password"
                        autoComplete="off"
                        placeholder={copy.endpointApiKeyPlaceholder}
                        value={newEndpointForm.api_key}
                        onChange={(e) => setNewEndpointForm({ ...newEndpointForm, api_key: e.target.value })}
                        required={createMode === "new"}
                      />
                    </div>
                  </TabsContent>
                </Tabs>
              </div>

              <div
                className="grid items-start gap-6 xl:grid-cols-[minmax(0,1.6fr)_minmax(19rem,0.9fr)]"
                data-testid="connection-dialog-main-grid"
              >
                <div className="space-y-5" data-testid="connection-dialog-left-column">
                  <div className="space-y-2">
                    <Label htmlFor="conn-name">{copy.connectionNameOptional}</Label>
                    <Input
                      id="conn-name"
                      name="name"
                      autoComplete="off"
                      placeholder={copy.connectionDisplayNamePlaceholder}
                      value={connectionForm.name || ""}
                      onChange={(e) => setConnectionForm({ ...connectionForm, name: e.target.value })}
                    />
                    <p className="text-[11px] text-muted-foreground">
                      {copy.useEndpointNameFallback(endpointSourceDefaultName)}
                    </p>
                    <p className="text-[11px] text-muted-foreground">{copy.routingPriorityHint}</p>
                  </div>

                  <div>
                    <SwitchController
                      label={copy.active}
                      description={copy.includeInLoadBalancing}
                      checked={connectionForm.is_active ?? true}
                      onCheckedChange={(checked) => setConnectionForm({ ...connectionForm, is_active: checked })}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="conn-pricing-template">{copy.pricingTemplate}</Label>
                    <Select
                      value={connectionForm.pricing_template_id ? String(connectionForm.pricing_template_id) : "unpriced"}
                      onValueChange={(value) => {
                        setConnectionForm({
                          ...connectionForm,
                          pricing_template_id: value === "unpriced" ? null : parseInt(value, 10),
                        });
                      }}
                    >
                      <SelectTrigger id="conn-pricing-template">
                        <SelectValue placeholder={copy.pricingTemplatePlaceholder} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="unpriced">{copy.unpricedNoCostTracking}</SelectItem>
                        {pricingTemplates.map((template) => (
                          <SelectItem key={template.id} value={String(template.id)}>
                            {template.name} v{template.version}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-[11px] text-muted-foreground">{copy.pricingTemplateHint}</p>
                  </div>
                </div>

                <div className="flex min-h-0 flex-col gap-4" data-testid="connection-dialog-right-column">
                  <div className="rounded-2xl border bg-muted/15 p-4" data-testid="connection-dialog-limiter-card">
                    <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-1">
                      {limiterFields.map((field) => (
                        <div key={field.field} className="grid content-start gap-1.5">
                          <div className="min-h-14 space-y-1">
                            <Label htmlFor={field.id}>{field.label}</Label>
                            <p className="text-[11px] text-muted-foreground">{copy.leaveBlankForUnlimited}</p>
                          </div>
                          <Input
                            id={field.id}
                            name={field.field}
                            type="number"
                            autoComplete="off"
                            min="0"
                            value={field.value ?? ""}
                            onChange={(e) => handleLimiterChange(field.field, e.target.value)}
                          />
                        </div>
                      ))}
                    </div>
                  </div>

                  <div
                    className="flex min-h-0 flex-col gap-3 rounded-2xl border bg-muted/10 p-4"
                    data-testid="connection-dialog-custom-headers-card"
                  >
                    <div className="flex items-center justify-between gap-3">
                      <Label>{copy.customHeaders}</Label>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => setHeaderRows([...headerRows, createHeaderRow()])}
                      >
                        <Plus className="mr-1.5 h-3 w-3" />
                        {copy.addHeader}
                      </Button>
                    </div>
                    <div className="min-h-0 overflow-hidden rounded-xl border border-dashed bg-background/70">
                      <ScrollArea
                        className="max-h-[min(32vh,22rem)] w-full"
                        data-testid="connection-dialog-custom-headers-scroll-area"
                      >
                        <div className="space-y-2 p-2 sm:p-3">
                          {headerRows.length === 0 ? (
                            <p className="px-1 py-2 text-xs italic text-muted-foreground">
                              {copy.noCustomHeadersConfigured}
                            </p>
                          ) : null}
                          {headerRows.map((row, index) => (
                            <div
                              key={row.id}
                              className="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-center"
                            >
                              <Input
                                id={`connection-header-key-${index}`}
                                name={`custom_headers.${index}.key`}
                                autoComplete="off"
                                aria-label={copy.headerKey}
                                placeholder={copy.headerKey}
                                value={row.key}
                                onChange={(e) => {
                                  const newRows = [...headerRows];
                                  newRows[index].key = e.target.value;
                                  setHeaderRows(newRows);
                                }}
                                className="flex-1"
                              />
                              <Input
                                id={`connection-header-value-${index}`}
                                name={`custom_headers.${index}.value`}
                                autoComplete="off"
                                aria-label={copy.headerValue}
                                placeholder={copy.headerValue}
                                value={row.value}
                                onChange={(e) => {
                                  const newRows = [...headerRows];
                                  newRows[index].value = e.target.value;
                                  setHeaderRows(newRows);
                                }}
                                className="flex-1"
                              />
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon"
                                onClick={() => {
                                  const newRows = [...headerRows];
                                  newRows.splice(index, 1);
                                  setHeaderRows(newRows);
                                }}
                              >
                                <X className="h-4 w-4" />
                              </Button>
                            </div>
                          ))}
                        </div>
                      </ScrollArea>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </ScrollArea>

          <div className="shrink-0 border-t bg-background px-6 py-4 sm:px-7">
            {dialogTestResult ? (
              <div
                className={cn(
                  "mb-3 rounded-md p-3 text-sm",
                  dialogTestResult.status === "healthy"
                    ? "bg-emerald-50 text-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
                    : "bg-red-50 text-red-900 dark:bg-red-950/30 dark:text-red-200",
                )}
              >
                <p className="font-medium">
                  {dialogTestResult.status === "healthy" ? copy.connectionHealthy : copy.connectionUnhealthy}
                </p>
                <p className="mt-1 text-xs opacity-90">{dialogTestResult.detail}</p>
              </div>
            ) : null}

            <DialogFooter className="gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={handleDialogTestConnection}
                disabled={dialogTestingConnection}
              >
                {dialogTestingConnection ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    {copy.testingConnection}
                  </>
                ) : (
                  copy.testConnection
                )}
              </Button>
              <div className="flex-1" />
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                {copy.cancel}
              </Button>
              <Button type="submit">{copy.saveConnection}</Button>
            </DialogFooter>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
