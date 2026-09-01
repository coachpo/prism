import type { FormEvent, ReactNode } from "react";
import { Plus, X } from "lucide-react";
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
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
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
import { classifyOpenAICoverage } from "./classifyOpenAICoverage";
import { ConnectionCustomRequestParametersEditor } from "./ConnectionCustomRequestParametersEditor";
import { ConnectionRoutingScheduleField } from "./ConnectionRoutingScheduleField";
import {
  routingScheduleDraftFromSchedule,
  type RoutingScheduleDraft,
  type RoutingScheduleDraftError,
} from "./routingScheduleDraft";
import {
  customRequestParametersDraftFromValue,
  parseCustomRequestParametersDraft,
  type CustomRequestParametersParseError,
} from "./customRequestParameters";
import {
  OperatorStatusBadge,
  OperatorSwitchField,
  OperatorTypeBadge,
} from "@/shared/design-system";
import type {
  ApiFamily,
  Connection,
  Endpoint,
  EndpointCreate,
  OpenAITextCapability,
  PricingTemplate,
} from "@/lib/types";
import {
  createHeaderRow,
  type ConnectionDialogForm,
  type HeaderRow,
} from "./useModelDetailDialogState";

interface ConnectionDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  apiFamily: ApiFamily | null;
  ownerOpenAIAcceptedFormat?: OpenAITextCapability | null;
  ownerModelId?: string | null;
  editingConnection: Connection | null;
  lockedEndpointId?: number | null;
  connectionForm: ConnectionDialogForm;
  setConnectionForm: (form: ConnectionDialogForm) => void;
  newEndpointForm: EndpointCreate;
  setNewEndpointForm: (form: EndpointCreate) => void;
  createMode: "select" | "new";
  setCreateMode: (mode: "select" | "new") => void;
  selectedEndpointId: string;
  setSelectedEndpointId: (id: string) => void;
  globalEndpoints: Endpoint[];
  headerRows: HeaderRow[];
  setHeaderRows: (rows: HeaderRow[]) => void;
  customRequestParametersDraft: string;
  routingScheduleDraft: RoutingScheduleDraft;
  setRoutingScheduleDraft: (draft: RoutingScheduleDraft) => void;
  routingScheduleError: RoutingScheduleDraftError | null;
  setCustomRequestParametersDraft: (draft: string) => void;
  customRequestParametersError: CustomRequestParametersParseError | null;
  setCustomRequestParametersError: (
    error: CustomRequestParametersParseError | null,
  ) => void;
  upstreamModelIdError: string | null;
  setUpstreamModelIdError: (error: string | null) => void;
  handleConnectionSubmit: (e: FormEvent<HTMLFormElement>) => Promise<void>;
  endpointSourceDefaultName: string | null;
  pricingTemplates: PricingTemplate[];
  prefillConnections?: Connection[];
  onPrefill?: (connection: Connection) => void;
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
      className={cn(
        "flex flex-col gap-3 border-b pb-4 last:border-b-0 last:pb-0",
        className,
      )}
      data-testid={dataTestId}
    >
      <div className="flex flex-col gap-0.5">
        <h2 className="text-sm font-semibold tracking-tight text-foreground">
          {title}
        </h2>
        {description ? (
          <p className="text-sm text-muted-foreground">{description}</p>
        ) : null}
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
    <div className={cn("flex flex-col gap-1.5", className)}>
      <Label htmlFor={id}>{label}</Label>
      {description ? (
        <p className="text-xs text-muted-foreground">{description}</p>
      ) : null}
      {children}
    </div>
  );
}

function pricingTemplateKindLabel(
  kind: PricingTemplate["template_kind"],
  copy: ReturnType<typeof useLocale>["messages"]["modelDetail"],
): string {
  if (kind === "standard") return copy.pricingTemplateKindStandard;
  if (kind === "tiered") return copy.pricingTemplateKindTiered;
  return copy.pricingTemplateKindPeakValley;
}

export function ConnectionDialog({
  isOpen,
  onOpenChange,
  apiFamily,
  ownerOpenAIAcceptedFormat,
  ownerModelId,
  editingConnection,
  lockedEndpointId,
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
  customRequestParametersDraft,
  routingScheduleDraft,
  setRoutingScheduleDraft,
  routingScheduleError,
  setCustomRequestParametersDraft,
  customRequestParametersError,
  setCustomRequestParametersError,
  upstreamModelIdError,
  setUpstreamModelIdError,
  handleConnectionSubmit,
  endpointSourceDefaultName,
  pricingTemplates,
  prefillConnections = [],
  onPrefill,
}: ConnectionDialogProps) {
  const { messages } = useLocale();
  const routingCopy = messages.routing;
  const copy = messages.modelDetail;
  const isOpenAI = apiFamily === "openai";
  const capabilityLockedToOwner = isOpenAI && ownerOpenAIAcceptedFormat != null;
  const upstreamModelIdValue = connectionForm.upstream_model_id ?? "";
  const upstreamModelIdHint =
    editingConnection && ownerModelId != null && upstreamModelIdValue.trim() !== ownerModelId.trim()
      ? copy.upstreamModelIdHintDecoupled(ownerModelId ?? "")
      : copy.upstreamModelIdHint(ownerModelId ?? "");
  const resolvedTextCapability = isOpenAI
    ? (connectionForm.openai_text_capability ?? "responses_only")
    : null;
  const isEndpointLocked = lockedEndpointId != null;
  const textCapabilityOptions: Array<{
    label: string;
    value: OpenAITextCapability;
  }> = [
    {
      value: "responses_only",
      label: copy.openaiTextCapabilityResponsesOnly,
    },
    {
      value: "chat_completions_only",
      label: copy.openaiTextCapabilityChatCompletionsOnly,
    },
    {
      value: "dual_native",
      label: copy.openaiTextCapabilityDualNative,
    },
  ];
  const visibleTextCapabilityOptions = capabilityLockedToOwner
    ? textCapabilityOptions.filter(
        (option) => option.value === ownerOpenAIAcceptedFormat,
      )
    : textCapabilityOptions;
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

  const updateConnectionForm = (nextForm: ConnectionDialogForm) => {
    setConnectionForm(nextForm);
  };

  // Prefill from an existing same-family Terminal Target: this only fills the
  // draft (endpoint reference, name, active, auth type, capability, pricing,
  // limits, headers). No IDs, positions, runtime state or endpoint keys are
  // copied; saving always creates an independent private Connection.
  const handlePrefill = (source: Connection) => {
    if (source.endpoint) {
      setSelectedEndpointId(String(source.endpoint.id));
    }
    updateConnectionForm({
      ...connectionForm,
      name: source.name ?? null,
      is_active: source.is_active,
      auth_type: source.auth_type ?? null,
      upstream_model_id: source.upstream_model_id ?? "",
      openai_text_capability: source.openai_text_capability ?? null,
      pricing_template_id: source.pricing_template_id ?? null,
      qps_limit: source.qps_limit ?? null,
      max_in_flight_non_stream: source.max_in_flight_non_stream ?? null,
      max_in_flight_stream: source.max_in_flight_stream ?? null,
    });
    setCustomRequestParametersDraft(
      customRequestParametersDraftFromValue(source.custom_request_parameters),
    );
    // The copy source's schedule must be carried too: without this, "copy into
    // a new connection" silently drops the windows the operator was copying.
    setRoutingScheduleDraft(
      routingScheduleDraftFromSchedule(source.routing_schedule),
    );
    setCustomRequestParametersError(null);
    setUpstreamModelIdError(null);
    setHeaderRows(
      Object.entries(source.custom_headers ?? {}).map(([key, value]) => ({
        id: `prefill-header-${key}`,
        key,
        value: String(value),
        redacted: (source.custom_headers_redacted ?? []).includes(key),
      })),
    );
  };

  const handlePrefillConnection = onPrefill ?? handlePrefill;

  const updateNewEndpointForm = (nextForm: EndpointCreate) => {
    setNewEndpointForm(nextForm);
  };

  const updateHeaderRows = (nextRows: HeaderRow[]) => {
    setHeaderRows(nextRows);
  };

  const handleCustomRequestParametersDraftChange = (nextDraft: string) => {
    setCustomRequestParametersDraft(nextDraft);
    setCustomRequestParametersError(
      parseCustomRequestParametersDraft(nextDraft).error,
    );
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

  const handleTextCapabilityChange = (value: string) => {
    if (capabilityLockedToOwner && value !== ownerOpenAIAcceptedFormat) return;
    updateConnectionForm({
      ...connectionForm,
      openai_text_capability: value as OpenAITextCapability,
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(92vh,64rem)] max-h-[92vh] max-w-6xl flex-col overflow-hidden p-0 sm:max-w-6xl">
        <DialogHeader className="shrink-0 border-b bg-background px-5 py-3.5 sm:px-6">
          <DialogTitle>
            {editingConnection ? copy.editConnection : copy.addConnection}
          </DialogTitle>
          <DialogDescription>
            {copy.connectionDialogDescription}
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={handleConnectionSubmit}
          className="flex min-h-0 flex-1 flex-col"
          noValidate
        >
          <input type="hidden" name="create_mode" value={createMode} />
          <input
            type="hidden"
            name="selected_endpoint_id"
            value={createMode === "select" ? selectedEndpointId : ""}
          />
          <input
            type="hidden"
            name="is_active"
            value={String(connectionForm.is_active ?? true)}
          />
          <input
            type="hidden"
            name="pricing_template_id"
            value={
              connectionForm.pricing_template_id === null
                ? ""
                : String(connectionForm.pricing_template_id)
            }
          />

          <DialogBody className="min-h-0 flex-1 p-0">
            <ScrollArea className="min-h-0 flex-1">
              <div
                className="px-5 py-4 sm:px-6"
                data-testid="connection-dialog-scroll-body"
              >
                <div
                  className="flex min-h-0 flex-col gap-4"
                  data-testid="connection-dialog-main-grid"
                >
                  <div
                    className="flex min-h-0 flex-col gap-4"
                    data-testid="connection-dialog-left-column"
                  >
                    <ConnectionDialogSection
                      title={copy.setup}
                      description={copy.setupDescription}
                      dataTestId="connection-dialog-setup-section"
                    >
                      {!editingConnection && prefillConnections.length > 0 ? (
                        <div
                          className="flex flex-col gap-2"
                          data-testid="connection-dialog-prefill"
                        >
                          <Label htmlFor="conn-prefill-source">
                            {copy.prefillFromExisting}
                          </Label>
                          <Select
                            value=""
                            onValueChange={(value) => {
                              const source = prefillConnections.find(
                                (candidate) => String(candidate.id) === value,
                              );
                              if (source) handlePrefillConnection(source);
                            }}
                          >
                            <SelectTrigger
                              id="conn-prefill-source"
                              className="w-full"
                            >
                              <SelectValue
                                placeholder={
                                  copy.prefillFromExistingPlaceholder
                                }
                              />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                {prefillConnections.map((candidate) => (
                                  <SelectItem
                                    key={candidate.id}
                                    value={String(candidate.id)}
                                  >
                                    {candidate.name ||
                                      candidate.endpoint?.name ||
                                      `终端目标 ${candidate.id}`}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </div>
                      ) : null}
                      <div
                        className="flex flex-col gap-3 border-b pb-3"
                        data-testid="connection-dialog-endpoint-source-section"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <Label className="text-sm font-medium">
                              {copy.endpointSource}
                            </Label>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {editingConnection
                                ? copy.endpointSourceEditHint
                                : copy.endpointSourceCreateHint}
                            </p>
                          </div>
                          <div className="flex items-center gap-2">
                            <OperatorTypeBadge
                              label={
                                createMode === "select"
                                  ? copy.selectExisting
                                  : copy.createNew
                              }
                              preserveLabel
                            />
                            {editingConnection ? (
                              <OperatorStatusBadge
                                label={copy.editable}
                                intent="accent"
                              />
                            ) : null}
                          </div>
                        </div>

                        <Tabs
                          value={createMode}
                          onValueChange={(value) => {
                            if (isEndpointLocked) return;
                            setCreateMode(value as "select" | "new");
                          }}
                          className="gap-3"
                        >
                          <TabsList className="grid w-full grid-cols-2 md:max-w-md">
                            <TabsTrigger
                              value="select"
                              disabled={
                                isEndpointLocked && createMode !== "select"
                              }
                            >
                              {copy.selectExisting}
                            </TabsTrigger>
                            <TabsTrigger
                              value="new"
                              disabled={isEndpointLocked}
                            >
                              {copy.createNew}
                            </TabsTrigger>
                          </TabsList>

                          <TabsContent
                            value="select"
                            className="flex flex-col gap-2"
                          >
                            <ConnectionDialogField
                              id="conn-selected-endpoint"
                              label={copy.selectEndpoint}
                            >
                              <Select
                                value={selectedEndpointId}
                                disabled={isEndpointLocked}
                                onValueChange={(value) => {
                                  setSelectedEndpointId(value);
                                }}
                              >
                                <SelectTrigger
                                  id="conn-selected-endpoint"
                                  disabled={isEndpointLocked}
                                >
                                  <SelectValue
                                    placeholder={copy.selectEndpointPlaceholder}
                                  />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectGroup>
                                    {globalEndpoints.map((endpoint) => (
                                      <SelectItem
                                        key={endpoint.id}
                                        value={String(endpoint.id)}
                                      >
                                        {endpoint.name} ({endpoint.base_url})
                                      </SelectItem>
                                    ))}
                                  </SelectGroup>
                                </SelectContent>
                              </Select>
                            </ConnectionDialogField>

                            {globalEndpoints.length === 0 ? (
                              <p className="text-xs text-muted-foreground">
                                {copy.noEndpointsFound}
                              </p>
                            ) : null}
                          </TabsContent>

                          <TabsContent
                            value="new"
                            className="grid gap-2.5 md:grid-cols-2"
                            data-testid="connection-dialog-create-new-grid"
                          >
                            <ConnectionDialogField
                              id="endpoint-name"
                              label={copy.endpointName}
                            >
                              <Input
                                id="endpoint-name"
                                name="endpoint_name"
                                autoComplete="off"
                                placeholder={copy.endpointNamePlaceholder}
                                value={newEndpointForm.name}
                                onChange={(e) =>
                                  updateNewEndpointForm({
                                    ...newEndpointForm,
                                    name: e.target.value,
                                  })
                                }
                                required={createMode === "new"}
                              />
                            </ConnectionDialogField>

                            <ConnectionDialogField
                              id="endpoint-base-url"
                              label={copy.endpointBaseUrl}
                            >
                              <Input
                                id="endpoint-base-url"
                                name="endpoint_base_url"
                                autoComplete="off"
                                placeholder={copy.endpointBaseUrlPlaceholder}
                                value={newEndpointForm.base_url}
                                onChange={(e) =>
                                  updateNewEndpointForm({
                                    ...newEndpointForm,
                                    base_url: e.target.value,
                                  })
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
                                  updateNewEndpointForm({
                                    ...newEndpointForm,
                                    api_key: e.target.value,
                                  })
                                }
                                required={createMode === "new"}
                              />
                            </ConnectionDialogField>
                          </TabsContent>
                        </Tabs>
                      </div>

                      <div
                        className="grid gap-3 border-b pb-3 md:grid-cols-2"
                        data-testid="connection-dialog-configuration-section"
                      >
                        <ConnectionDialogField
                          id="conn-name"
                          label={copy.connectionNameOptional}
                          description={copy.useEndpointNameFallback(
                            endpointSourceDefaultName,
                          )}
                          className="md:col-span-2"
                        >
                          <Input
                            id="conn-name"
                            name="name"
                            autoComplete="off"
                            placeholder={copy.connectionDisplayNamePlaceholder}
                            value={connectionForm.name || ""}
                            onChange={(e) =>
                              updateConnectionForm({
                                ...connectionForm,
                                name: e.target.value,
                              })
                            }
                          />
                        </ConnectionDialogField>

                        <Field data-invalid={upstreamModelIdError != null} className="gap-1.5 md:col-span-2">
                          <FieldLabel htmlFor="conn-upstream-model-id">{copy.upstreamModelId}</FieldLabel>
                          <Input
                            id="conn-upstream-model-id"
                            name="upstream_model_id"
                            autoComplete="off"
                            className="font-mono"
                            placeholder={copy.upstreamModelIdPlaceholder}
                            aria-invalid={upstreamModelIdError != null}
                            value={upstreamModelIdValue}
                            onChange={(event) => {
                              setUpstreamModelIdError(null);
                              updateConnectionForm({
                                ...connectionForm,
                                upstream_model_id: event.target.value,
                              });
                            }}
                          />
                          {upstreamModelIdError ? (
                            <FieldError>{upstreamModelIdError}</FieldError>
                          ) : (
                            <FieldDescription>{upstreamModelIdHint}</FieldDescription>
                          )}
                        </Field>

                        <OperatorSwitchField
                          label={copy.active}
                          description={copy.includeInLoadBalancing}
                          checked={connectionForm.is_active ?? true}
                          onCheckedChange={(checked) =>
                            updateConnectionForm({
                              ...connectionForm,
                              is_active: checked,
                            })
                          }
                        />

                        {apiFamily === "gemini" ? (
                          <ConnectionDialogField
                            id="conn-auth-type"
                            label={copy.authType}
                            description={copy.authTypeGeminiHint}
                          >
                            <Select
                              value={connectionForm.auth_type ?? "gemini"}
                              onValueChange={(value) =>
                                updateConnectionForm({
                                  ...connectionForm,
                                  auth_type: value,
                                })
                              }
                            >
                              <SelectTrigger id="conn-auth-type">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="gemini">
                                  {copy.authTypeGeminiBearer}
                                </SelectItem>
                                <SelectItem value="gemini_api_key">
                                  {copy.authTypeGeminiAPIKey}
                                </SelectItem>
                              </SelectContent>
                            </Select>
                          </ConnectionDialogField>
                        ) : null}

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
                                pricing_template_id:
                                  value === "unpriced"
                                    ? null
                                    : parseInt(value, 10),
                              });
                            }}
                          >
                            <SelectTrigger id="conn-pricing-template">
                              <SelectValue
                                placeholder={copy.pricingTemplatePlaceholder}
                              />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                <SelectItem value="unpriced">
                                  {copy.unpricedNoCostTracking}
                                </SelectItem>
                                {pricingTemplates.map((template) => (
                                  <SelectItem
                                    key={template.id}
                                    value={String(template.id)}
                                  >
                                    {template.name} ·{" "}
                                    {pricingTemplateKindLabel(
                                      template.template_kind,
                                      copy,
                                    )}{" "}
                                    · v{template.version}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </ConnectionDialogField>
                      </div>

                      <p className="text-xs text-muted-foreground">
                        {copy.routingPriorityHint}
                      </p>
                    </ConnectionDialogSection>

                    {isOpenAI ? (
                      <ConnectionDialogSection
                        title={copy.openaiTextCapability}
                        description={copy.openaiTextCapabilityDescription}
                        dataTestId="connection-dialog-openai-capability-section"
                      >
                        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,0.8fr)]">
                          <ConnectionDialogField
                            id="conn-openai-text-capability"
                            label={copy.openaiTextCapabilitySelector}
                          >
                            <Select
                              value={resolvedTextCapability ?? "responses_only"}
                              onValueChange={handleTextCapabilityChange}
                              disabled={capabilityLockedToOwner}
                            >
                              <SelectTrigger id="conn-openai-text-capability">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectGroup>
                                  {visibleTextCapabilityOptions.map(
                                    (option) => (
                                      <SelectItem
                                        key={option.value}
                                        value={option.value}
                                      >
                                        {option.label}
                                      </SelectItem>
                                    ),
                                  )}
                                </SelectGroup>
                              </SelectContent>
                            </Select>
                          </ConnectionDialogField>

                          <div
                            className="flex flex-col gap-1.5 border-l pl-3"
                            data-testid="connection-dialog-capability-preview"
                          >
                            <p className="text-xs font-medium text-muted-foreground">
                              {routingCopy.capabilityCoverageLabel}
                            </p>
                            {(() => {
                              const preview = classifyOpenAICoverage(
                                ownerOpenAIAcceptedFormat,
                                resolvedTextCapability ?? null,
                              );
                              const badgeLabel =
                                preview.coverage === "full"
                                  ? routingCopy.coverageFull
                                  : preview.coverage === "partial"
                                    ? routingCopy.coveragePartial
                                    : routingCopy.coverageNone;
                              const badgeIntent =
                                preview.coverage === "full"
                                  ? "healthy"
                                  : preview.coverage === "partial"
                                    ? "degraded"
                                    : "danger";
                              return (
                                <div className="flex flex-col gap-1">
                                  <OperatorStatusBadge
                                    intent={badgeIntent}
                                    label={badgeLabel}
                                    preserveLabel
                                  />
                                  {preview.unsupportedAcceptedOperations
                                    .length > 0 ? (
                                    <p className="text-xs text-muted-foreground">
                                      {routingCopy.missingOperations(
                                        preview.unsupportedAcceptedOperations.join(
                                          "、",
                                        ),
                                      )}
                                    </p>
                                  ) : null}
                                </div>
                              );
                            })()}
                          </div>
                        </div>
                      </ConnectionDialogSection>
                    ) : null}

                    <ConnectionDialogSection
                      title={copy.advancedRequestSettings}
                      description={copy.advancedRequestSettingsDescription}
                      dataTestId="connection-dialog-advanced-section"
                    >
                      <div className="grid gap-3 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
                        <section
                          className="flex flex-col gap-3 border-b pb-3 xl:border-r xl:border-b-0 xl:pr-3 xl:pb-0"
                          data-testid="connection-dialog-limiter-card"
                        >
                          <div className="flex flex-col gap-1">
                            <h3 className="text-sm font-semibold tracking-tight text-foreground">
                              {copy.qpsLimit}
                            </h3>
                            <p className="text-sm text-muted-foreground">
                              {copy.leaveBlankForUnlimited}
                            </p>
                          </div>

                          <div className="grid gap-2.5 sm:grid-cols-3 xl:grid-cols-1">
                            {limiterFields.map((field) => (
                              <ConnectionDialogField
                                key={field.field}
                                id={field.id}
                                label={field.label}
                              >
                                <Input
                                  id={field.id}
                                  name={field.field}
                                  type="number"
                                  autoComplete="off"
                                  min="1"
                                  step="1"
                                  value={field.value ?? ""}
                                  onChange={(e) =>
                                    handleLimiterChange(
                                      field.field,
                                      e.target.value,
                                    )
                                  }
                                />
                              </ConnectionDialogField>
                            ))}
                          </div>
                        </section>

                        <section
                          className="flex flex-col gap-3"
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
                              onClick={() =>
                                updateHeaderRows([
                                  ...headerRows,
                                  createHeaderRow(),
                                ])
                              }
                            >
                              <Plus data-icon="inline-start" />
                              {copy.addHeader}
                            </Button>
                          </div>

                          <div className="border-t pt-2">
                            <div className="flex flex-col gap-2">
                              {headerRows.length === 0 ? (
                                <p className="py-1.5 text-xs italic text-muted-foreground">
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
                                    type={row.redacted ? "password" : undefined}
                                    value={
                                      row.redacted && !row.value
                                        ? ""
                                        : row.value
                                    }
                                    onChange={(e) => {
                                      const nextRows = [...headerRows];
                                      nextRows[index].value = e.target.value;
                                      nextRows[index].redacted = false;
                                      updateHeaderRows(nextRows);
                                    }}
                                    className="flex-1"
                                  />
                                  {row.redacted ? (
                                    <span className="text-[11px] text-muted-foreground">
                                      {copy.customHeaderRedactedHint}
                                    </span>
                                  ) : null}
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

                      <ConnectionCustomRequestParametersEditor
                        draft={customRequestParametersDraft}
                        onDraftChange={handleCustomRequestParametersDraftChange}
                        error={customRequestParametersError}
                      />
                    </ConnectionDialogSection>

                    {/*
                      A sibling section, not a member of the advanced request
                      settings group: that group is described as request
                      settings, while a routing window decides routing
                      eligibility. Nesting it there would make the group's own
                      description untrue.
                    */}
                    <ConnectionRoutingScheduleField
                      draft={routingScheduleDraft}
                      onDraftChange={setRoutingScheduleDraft}
                      error={routingScheduleError}
                    />
                  </div>
                </div>
              </div>
            </ScrollArea>
          </DialogBody>

          <div className="shrink-0 border-t bg-background px-5 py-3 sm:px-6">
            <DialogFooter className="pt-0">
              <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => onOpenChange(false)}
                >
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
