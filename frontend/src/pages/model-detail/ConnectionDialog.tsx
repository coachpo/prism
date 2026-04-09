import type { FormEvent, ReactNode } from "react";
import { Loader2, Plus, X } from "lucide-react";
import { SwitchController } from "@/components/SwitchController";
import { StatusBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  PricingTemplate,
} from "@/lib/types";
import {
  decomposeOpenAIProbeVariant,
  normalizeOpenAIProbeEndpointVariant,
  resolveOpenAIProbeVariant,
  type OpenAIProbeApi,
  type OpenAIProbeReasoningMode,
} from "./connectionProbeBehavior";
import { normalizeConnectionHeaders } from "./useModelDetailDataSupport";
import { createHeaderRow } from "./useModelDetailDialogState";
import type { HeaderRow } from "./useModelDetailDialogState";

interface ConnectionDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  apiFamily: ApiFamily | null;
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
  handleConnectionSubmit: (e: FormEvent<HTMLFormElement>) => Promise<void>;
  dialogTestingConnection: boolean;
  dialogTestResult: { status: string; detail: string } | null;
  clearDialogTestResult: () => void;
  handleDialogTestConnection: () => Promise<void>;
  endpointSourceDefaultName: string | null;
  pricingTemplates: PricingTemplate[];
}

interface ConnectionDialogSectionProps {
  children: ReactNode;
  className?: string;
  dataTestId?: string;
  description?: string;
  title: string;
}

function ConnectionDialogSection({
  children,
  className,
  dataTestId,
  description,
  title,
}: ConnectionDialogSectionProps) {
  return (
    <section
      className={cn("flex flex-col gap-4 rounded-2xl border bg-muted/15 p-4 sm:p-5", className)}
      data-testid={dataTestId}
    >
      <div className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold tracking-tight text-foreground">{title}</h2>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {children}
    </section>
  );
}

interface ConnectionDialogFieldProps {
  children: ReactNode;
  className?: string;
  description?: string;
  id: string;
  label: string;
}

function ConnectionDialogField({
  children,
  className,
  description,
  id,
  label,
}: ConnectionDialogFieldProps) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <Label htmlFor={id}>{label}</Label>
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      {children}
    </div>
  );
}

function ConnectionSummaryItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <p className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{label}</p>
      {children}
    </div>
  );
}

export function ConnectionDialog({
  isOpen,
  onOpenChange,
  apiFamily,
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
  clearDialogTestResult,
  handleDialogTestConnection,
  endpointSourceDefaultName,
  pricingTemplates,
}: ConnectionDialogProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const isOpenAI = apiFamily === "openai";
  const selectedEndpoint = globalEndpoints.find((endpoint) => String(endpoint.id) === selectedEndpointId) ?? null;
  const resolvedProbeVariant = isOpenAI
    ? normalizeOpenAIProbeEndpointVariant(connectionForm.openai_probe_endpoint_variant)
    : null;
  const probeBehavior = decomposeOpenAIProbeVariant(resolvedProbeVariant);

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

  const summaryEndpointName =
    createMode === "select"
      ? selectedEndpoint?.name ?? copy.unknownEndpoint
      : newEndpointForm.name.trim() || copy.unknownEndpoint;
  const summaryEndpointUrl =
    createMode === "select"
      ? selectedEndpoint?.base_url ?? null
      : newEndpointForm.base_url.trim() || null;
  const resolvedConnectionName =
    (connectionForm.name ?? "").trim() || endpointSourceDefaultName || copy.unassigned;
  const selectedPricingTemplate = pricingTemplates.find(
    (template) => template.id === connectionForm.pricing_template_id,
  );
  const pricingSummary = selectedPricingTemplate
    ? `${selectedPricingTemplate.name} v${selectedPricingTemplate.version}`
    : copy.unpricedNoCostTracking;
  const normalizedHeaders = normalizeConnectionHeaders(headerRows);
  const customHeaderCount = normalizedHeaders ? Object.keys(normalizedHeaders).length : 0;
  const probeBehaviorSummary = isOpenAI
    ? `${
        probeBehavior.probeApi === "responses"
          ? copy.probeApiResponses
          : copy.probeApiChatCompletions
      } • ${
        probeBehavior.reasoningMode === "default"
          ? copy.reasoningHandlingDefault
          : copy.reasoningHandlingDisabled
      }`
    : null;

  const updateConnectionForm = (nextForm: ConnectionCreate) => {
    clearDialogTestResult();
    setConnectionForm(nextForm);
  };

  const updateNewEndpointForm = (nextForm: EndpointCreate) => {
    clearDialogTestResult();
    setNewEndpointForm(nextForm);
  };

  const updateHeaderRows = (nextRows: HeaderRow[]) => {
    clearDialogTestResult();
    setHeaderRows(nextRows);
  };

  const handleLimiterChange = (
    field: "qps_limit" | "max_in_flight_non_stream" | "max_in_flight_stream",
    rawValue: string,
  ) => {
    const nextValue = rawValue === "" ? null : Number.parseInt(rawValue, 10);
    updateConnectionForm({
      ...connectionForm,
      [field]: Number.isNaN(nextValue) ? null : nextValue,
    });
  };

  const handleProbeApiChange = (value: string) => {
    updateConnectionForm({
      ...connectionForm,
      openai_probe_endpoint_variant: resolveOpenAIProbeVariant({
        probeApi: value as OpenAIProbeApi,
        reasoningMode: probeBehavior.reasoningMode,
      }),
    });
  };

  const handleReasoningModeChange = (value: string) => {
    updateConnectionForm({
      ...connectionForm,
      openai_probe_endpoint_variant: resolveOpenAIProbeVariant({
        probeApi: probeBehavior.probeApi,
        reasoningMode: value as OpenAIProbeReasoningMode,
      }),
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(94vh,68rem)] max-h-[94vh] max-w-6xl flex-col overflow-hidden p-0 sm:max-w-6xl">
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

          <DialogBody className="min-h-0 flex-1 p-0">
            <ScrollArea className="min-h-0 flex-1">
              <div className="px-6 py-5 sm:px-7" data-testid="connection-dialog-scroll-body">
                <div
                  className="grid items-start gap-6 xl:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.95fr)]"
                  data-testid="connection-dialog-main-grid"
                >
                  <div className="flex min-h-0 flex-col gap-6" data-testid="connection-dialog-left-column">
                    <ConnectionDialogSection
                      title={copy.setup}
                      description={copy.setupDescription}
                      dataTestId="connection-dialog-setup-section"
                    >
                      <div
                        className="flex flex-col gap-4 rounded-xl border bg-background/80 p-4"
                        data-testid="connection-dialog-endpoint-source-section"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <Label className="text-sm font-medium">{copy.endpointSource}</Label>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {editingConnection ? copy.endpointSourceEditHint : copy.endpointSourceCreateHint}
                            </p>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge variant="outline">
                              {createMode === "select" ? copy.selectExisting : copy.createNew}
                            </Badge>
                            {editingConnection ? <StatusBadge label={copy.editable} intent="info" /> : null}
                          </div>
                        </div>

                        <Tabs
                          value={createMode}
                          onValueChange={(value) => {
                            clearDialogTestResult();
                            setCreateMode(value as "select" | "new");
                          }}
                          className="gap-4"
                        >
                          <TabsList className="grid w-full grid-cols-2 md:max-w-md">
                            <TabsTrigger value="select">{copy.selectExisting}</TabsTrigger>
                            <TabsTrigger value="new">{copy.createNew}</TabsTrigger>
                          </TabsList>

                          <TabsContent value="select" className="flex flex-col gap-2.5">
                            <ConnectionDialogField id="conn-selected-endpoint" label={copy.selectEndpoint}>
                              <Select value={selectedEndpointId} onValueChange={(value) => {
                                clearDialogTestResult();
                                setSelectedEndpointId(value);
                              }}>
                                <SelectTrigger id="conn-selected-endpoint">
                                  <SelectValue placeholder={copy.selectEndpointPlaceholder} />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectGroup>
                                    {globalEndpoints.map((endpoint) => (
                                      <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                                        {endpoint.name} ({endpoint.base_url})
                                      </SelectItem>
                                    ))}
                                  </SelectGroup>
                                </SelectContent>
                              </Select>
                            </ConnectionDialogField>

                            {globalEndpoints.length === 0 ? (
                              <p className="text-xs text-muted-foreground">{copy.noProfileEndpointsFound}</p>
                            ) : null}
                          </TabsContent>

                          <TabsContent
                            value="new"
                            className="grid gap-3 md:grid-cols-2"
                            data-testid="connection-dialog-create-new-grid"
                          >
                            <ConnectionDialogField id="endpoint-name" label={copy.endpointName}>
                              <Input
                                id="endpoint-name"
                                name="endpoint_name"
                                autoComplete="off"
                                placeholder={copy.endpointNamePlaceholder}
                                value={newEndpointForm.name}
                                onChange={(e) =>
                                  updateNewEndpointForm({ ...newEndpointForm, name: e.target.value })
                                }
                                required={createMode === "new"}
                              />
                            </ConnectionDialogField>

                            <ConnectionDialogField id="endpoint-base-url" label={copy.endpointBaseUrl}>
                              <Input
                                id="endpoint-base-url"
                                name="endpoint_base_url"
                                autoComplete="off"
                                placeholder={copy.endpointBaseUrlPlaceholder}
                                value={newEndpointForm.base_url}
                                onChange={(e) =>
                                  updateNewEndpointForm({ ...newEndpointForm, base_url: e.target.value })
                                }
                                required={createMode === "new"}
                              />
                            </ConnectionDialogField>

                            <ConnectionDialogField
                              id="endpoint-api-key"
                              label={copy.endpointApiKey}
                              className="md:col-span-2"
                            >
                              <Input
                                id="endpoint-api-key"
                                name="endpoint_api_key"
                                type="password"
                                autoComplete="off"
                                placeholder={copy.endpointApiKeyPlaceholder}
                                value={newEndpointForm.api_key}
                                onChange={(e) =>
                                  updateNewEndpointForm({ ...newEndpointForm, api_key: e.target.value })
                                }
                                required={createMode === "new"}
                              />
                            </ConnectionDialogField>
                          </TabsContent>
                        </Tabs>
                      </div>

                      <div
                        className="grid gap-4 rounded-xl border bg-background/80 p-4 md:grid-cols-2"
                        data-testid="connection-dialog-configuration-section"
                      >
                        <ConnectionDialogField
                          id="conn-name"
                          label={copy.connectionNameOptional}
                          description={copy.useEndpointNameFallback(endpointSourceDefaultName)}
                          className="md:col-span-2"
                        >
                          <Input
                            id="conn-name"
                            name="name"
                            autoComplete="off"
                            placeholder={copy.connectionDisplayNamePlaceholder}
                            value={connectionForm.name || ""}
                            onChange={(e) =>
                              updateConnectionForm({ ...connectionForm, name: e.target.value })
                            }
                          />
                        </ConnectionDialogField>

                        <SwitchController
                          label={copy.active}
                          description={copy.includeInLoadBalancing}
                          checked={connectionForm.is_active ?? true}
                          onCheckedChange={(checked) =>
                            updateConnectionForm({ ...connectionForm, is_active: checked })
                          }
                        />

                        <ConnectionDialogField
                          id="conn-pricing-template"
                          label={copy.pricingTemplate}
                          description={copy.pricingTemplateHint}
                        >
                          <Select
                            value={
                              connectionForm.pricing_template_id
                                ? String(connectionForm.pricing_template_id)
                                : "unpriced"
                            }
                            onValueChange={(value) => {
                              updateConnectionForm({
                                ...connectionForm,
                                pricing_template_id: value === "unpriced" ? null : parseInt(value, 10),
                              });
                            }}
                          >
                            <SelectTrigger id="conn-pricing-template">
                              <SelectValue placeholder={copy.pricingTemplatePlaceholder} />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                <SelectItem value="unpriced">{copy.unpricedNoCostTracking}</SelectItem>
                                {pricingTemplates.map((template) => (
                                  <SelectItem key={template.id} value={String(template.id)}>
                                    {template.name} v{template.version}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </ConnectionDialogField>
                      </div>

                      <p className="text-xs text-muted-foreground">{copy.routingPriorityHint}</p>
                    </ConnectionDialogSection>

                    {isOpenAI ? (
                      <ConnectionDialogSection
                        title={copy.probeBehavior}
                        description={copy.probeBehaviorDescription}
                        dataTestId="connection-dialog-probe-section"
                      >
                        <div className="grid gap-4 rounded-xl border bg-background/80 p-4 md:grid-cols-2">
                          <ConnectionDialogField
                            id="conn-probe-api"
                            label={copy.probeApi}
                            description={
                              probeBehavior.probeApi === "responses"
                                ? copy.probeApiResponsesHint
                                : copy.probeApiChatCompletionsHint
                            }
                          >
                            <Select value={probeBehavior.probeApi} onValueChange={handleProbeApiChange}>
                              <SelectTrigger id="conn-probe-api">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectGroup>
                                  <SelectItem value="responses">{copy.probeApiResponses}</SelectItem>
                                  <SelectItem value="chat_completions">
                                    {copy.probeApiChatCompletions}
                                  </SelectItem>
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                          </ConnectionDialogField>

                          <ConnectionDialogField
                            id="conn-probe-reasoning-mode"
                            label={copy.reasoningHandling}
                            description={
                              probeBehavior.reasoningMode === "default"
                                ? copy.reasoningHandlingDefaultHint
                                : copy.reasoningHandlingDisabledHint
                            }
                          >
                            <Select value={probeBehavior.reasoningMode} onValueChange={handleReasoningModeChange}>
                              <SelectTrigger id="conn-probe-reasoning-mode">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectGroup>
                                  <SelectItem value="default">{copy.reasoningHandlingDefault}</SelectItem>
                                  <SelectItem value="disabled">{copy.reasoningHandlingDisabled}</SelectItem>
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                          </ConnectionDialogField>
                        </div>

                        <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-background/80 p-4">
                          <div className="flex min-w-0 flex-col gap-1">
                            <p className="text-xs font-medium text-muted-foreground">
                              {copy.probeBehaviorSummaryLabel}
                            </p>
                            <p className="text-sm text-foreground">{probeBehaviorSummary}</p>
                          </div>
                        </div>
                      </ConnectionDialogSection>
                    ) : null}

                    <ConnectionDialogSection
                      title={copy.advancedRequestSettings}
                      description={copy.advancedRequestSettingsDescription}
                      dataTestId="connection-dialog-advanced-section"
                    >
                      <div className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
                        <section
                          className="flex flex-col gap-4 rounded-xl border bg-background/80 p-4"
                          data-testid="connection-dialog-limiter-card"
                        >
                          <div className="flex flex-col gap-1">
                            <h3 className="text-sm font-semibold tracking-tight text-foreground">
                              {copy.qpsLimit}
                            </h3>
                            <p className="text-sm text-muted-foreground">{copy.leaveBlankForUnlimited}</p>
                          </div>

                          <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-1">
                            {limiterFields.map((field) => (
                              <ConnectionDialogField key={field.field} id={field.id} label={field.label}>
                                <Input
                                  id={field.id}
                                  name={field.field}
                                  type="number"
                                  autoComplete="off"
                                  min="0"
                                  value={field.value ?? ""}
                                  onChange={(e) => handleLimiterChange(field.field, e.target.value)}
                                />
                              </ConnectionDialogField>
                            ))}
                          </div>
                        </section>

                        <section
                          className="flex flex-col gap-3 rounded-xl border bg-background/80 p-4"
                          data-testid="connection-dialog-custom-headers-card"
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="flex flex-col gap-1">
                              <h3 className="text-sm font-semibold tracking-tight text-foreground">
                                {copy.customHeaders}
                              </h3>
                              <p className="text-sm text-muted-foreground">
                                {copy.customHeadersDescription}
                              </p>
                            </div>
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              onClick={() => updateHeaderRows([...headerRows, createHeaderRow()])}
                            >
                              <Plus data-icon="inline-start" />
                              {copy.addHeader}
                            </Button>
                          </div>

                          <div className="rounded-xl border border-dashed bg-background/70">
                            <div className="flex flex-col gap-2 p-2 sm:p-3">
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
                                      const nextRows = [...headerRows];
                                      nextRows[index].key = e.target.value;
                                      updateHeaderRows(nextRows);
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
                                      const nextRows = [...headerRows];
                                      nextRows[index].value = e.target.value;
                                      updateHeaderRows(nextRows);
                                    }}
                                    className="flex-1"
                                  />
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    aria-label={copy.removeHeader}
                                    onClick={() => {
                                      const nextRows = [...headerRows];
                                      nextRows.splice(index, 1);
                                      updateHeaderRows(nextRows);
                                    }}
                                  >
                                    <X />
                                  </Button>
                                </div>
                              ))}
                            </div>
                          </div>
                        </section>
                      </div>
                    </ConnectionDialogSection>
                  </div>

                  <div className="flex min-h-0 flex-col gap-4" data-testid="connection-dialog-right-column">
                    <ConnectionDialogSection
                      title={copy.summaryAndTest}
                      description={copy.summaryAndTestDescription}
                      dataTestId="connection-dialog-summary-panel"
                    >
                      <div className="flex flex-wrap gap-2">
                        <StatusBadge
                          label={(connectionForm.is_active ?? true) ? copy.enabled : copy.disabled}
                          intent={(connectionForm.is_active ?? true) ? "success" : "muted"}
                        />
                        <StatusBadge
                          label={selectedPricingTemplate ? copy.pricingOn : copy.pricingOff}
                          intent={selectedPricingTemplate ? "blue" : "muted"}
                        />
                        <Badge variant="outline">
                          {createMode === "select" ? copy.selectExisting : copy.createNew}
                        </Badge>
                      </div>

                      <ConnectionSummaryItem label={copy.endpointSummaryLabel}>
                        <div className="flex flex-col gap-1">
                          <p className="text-sm font-medium text-foreground">{summaryEndpointName}</p>
                          {summaryEndpointUrl ? (
                            <p className="text-xs text-muted-foreground break-all">{summaryEndpointUrl}</p>
                          ) : null}
                        </div>
                      </ConnectionSummaryItem>

                      <ConnectionSummaryItem label={copy.connectionNameSummaryLabel}>
                        <p className="text-sm text-foreground">{resolvedConnectionName}</p>
                      </ConnectionSummaryItem>

                      <ConnectionSummaryItem label={copy.pricingSummaryLabel}>
                        <p className="text-sm text-foreground">{pricingSummary}</p>
                      </ConnectionSummaryItem>

                      {isOpenAI && resolvedProbeVariant ? (
                        <ConnectionSummaryItem label={copy.probeBehaviorSummaryLabel}>
                          <div className="flex flex-col gap-2">
                            <p className="text-sm text-foreground">{probeBehaviorSummary}</p>
                          </div>
                        </ConnectionSummaryItem>
                      ) : null}

                      <ConnectionSummaryItem label={copy.customHeaders}>
                        <p className="text-sm text-foreground">
                          {customHeaderCount > 0
                            ? copy.customHeadersConfigured(String(customHeaderCount))
                            : copy.noCustomHeadersConfigured}
                        </p>
                      </ConnectionSummaryItem>
                    </ConnectionDialogSection>

                    <ConnectionDialogSection
                      title={copy.healthCheck}
                      description={copy.healthTestDescription}
                      dataTestId="connection-dialog-health-test-panel"
                    >
                      <Button
                        type="button"
                        variant="outline"
                        onClick={handleDialogTestConnection}
                        disabled={dialogTestingConnection}
                      >
                        {dialogTestingConnection ? (
                          <>
                            <Loader2 data-icon="inline-start" className="animate-spin" />
                            {copy.testingConnection}
                          </>
                        ) : (
                          copy.testConnection
                        )}
                      </Button>

                      {dialogTestResult ? (
                        <div
                          className={cn(
                            "rounded-xl border p-3 text-sm",
                            dialogTestResult.status === "healthy"
                              ? "border-emerald-500/25 bg-emerald-50 text-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
                              : "border-red-500/25 bg-red-50 text-red-900 dark:bg-red-950/30 dark:text-red-200",
                          )}
                          data-testid="connection-dialog-test-result"
                        >
                          <p className="font-medium">
                            {dialogTestResult.status === "healthy"
                              ? copy.connectionHealthy
                              : copy.connectionUnhealthy}
                          </p>
                          <p className="mt-1 text-xs opacity-90">{dialogTestResult.detail}</p>
                        </div>
                      ) : null}
                    </ConnectionDialogSection>
                  </div>
                </div>
              </div>
            </ScrollArea>
          </DialogBody>

          <div className="shrink-0 border-t bg-background px-6 py-4 sm:px-7">
            <DialogFooter className="pt-0">
              <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                  {copy.cancel}
                </Button>
                <Button type="submit">{copy.saveConnection}</Button>
              </div>
            </DialogFooter>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
