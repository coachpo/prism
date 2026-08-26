import { useEffect, useMemo, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { Button } from "@/components/ui/button";
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { endpoints as endpointsApi } from "@/lib/api/endpoints";
import { models as modelsApi } from "@/lib/api/models";
import type { Endpoint, LoadbalanceStrategy, ModelConfigListItem, OpenAIAcceptedFormat, OpenAIImageOperations } from "@/lib/types";
import type { ModelConfigCompositeCreate } from "@/lib/types";
import { getLoadbalanceStrategyTypeLabel } from "@/lib/loadbalanceRoutingPolicy";
import { OperatorCallout, OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system";
import { DEFAULT_OPENAI_ACCEPTED_FORMAT } from "@/pages/models/modelFormState";
import type { OpenAICapabilitySelectValue } from "@/features/models/openaiCapabilityOptions";
import {
  OPENAI_ACCEPTED_FORMAT_SELECT_VALUES,
  OPENAI_CAPABILITY_UNSET,
  OPENAI_IMAGE_OPERATIONS_SELECT_VALUES,
  fromSelectValue,
  getOpenAIAcceptedFormatOptionLabel,
  getOpenAIImageOperationsOptionLabel,
} from "@/features/models/openaiCapabilityOptions";

/**
 * One-step model creation (MC-B1): model identity plus the first Terminal
 * Target in a single composite create. "稍后配置" creates a disabled model
 * without a target. OpenAI capability is read-only and always follows the
 * owner accepted format.
 */
export function CreateModelDialog({
  isOpen,
  loadbalanceStrategies,
  onClose,
  onCreated,
  createLoadbalanceStrategyDefaultsPending = false,
  onCreateLoadbalanceStrategyDefaults,
}: {
  isOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  onClose: () => void;
  onCreated: (model: ModelConfigListItem) => void;
  createLoadbalanceStrategyDefaultsPending?: boolean;
  onCreateLoadbalanceStrategyDefaults?: () => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const [apiFamily, setApiFamily] = useState<"openai" | "anthropic" | "gemini">("openai");
  const [modelId, setModelId] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [acceptedFormat, setAcceptedFormat] = useState<OpenAICapabilitySelectValue<OpenAIAcceptedFormat>>(DEFAULT_OPENAI_ACCEPTED_FORMAT);
  const [imageOperations, setImageOperations] = useState<OpenAICapabilitySelectValue<OpenAIImageOperations>>(OPENAI_CAPABILITY_UNSET);
  const [strategyId, setStrategyId] = useState<number | null>(loadbalanceStrategies[0]?.id ?? null);
  const [configureLater, setConfigureLater] = useState(false);
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [endpointId, setEndpointId] = useState<number | null>(null);
  const [inlineEndpoint, setInlineEndpoint] = useState(false);
  const [inlineName, setInlineName] = useState("");
  const [inlineBaseUrl, setInlineBaseUrl] = useState("");
  const [inlineApiKey, setInlineApiKey] = useState("");
  const [targetName, setTargetName] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setFormError(null);
    setSubmitting(false);
    setConfigureLater(false);
    setEndpointId(null);
    setInlineEndpoint(false);
    if (loadbalanceStrategies.length > 0 && strategyId === null) {
      setStrategyId(loadbalanceStrategies[0].id);
    }
    void endpointsApi.list().then(setEndpoints).catch(() => setEndpoints([]));
  }, [isOpen, loadbalanceStrategies, strategyId]);

  const resolvedAcceptedFormat = fromSelectValue<OpenAIAcceptedFormat>(acceptedFormat) || null;
  const resolvedImageOperations = fromSelectValue<OpenAIImageOperations>(imageOperations) || null;
  // The initial Terminal Target derives both capabilities from the owner model
  // server-side, so only the text one is echoed back to the operator here.
  const capability = apiFamily === "openai" ? resolvedAcceptedFormat : null;

  const strategyById = useMemo(() => {
    const map = new Map<number, LoadbalanceStrategy>();
    for (const strategy of loadbalanceStrategies) map.set(strategy.id, strategy);
    return map;
  }, [loadbalanceStrategies]);

  const handleSubmit = async () => {
    setFormError(null);
    if (!modelId.trim()) {
      setFormError(messages.modelsData.modelIdRequired);
      return;
    }
    if (strategyId === null) {
      setFormError(copy.selectLoadbalanceStrategy ?? messages.modelsData.selectLoadbalanceStrategy);
      return;
    }
    if (!configureLater) {
      if (!inlineEndpoint && endpointId === null) {
        setFormError(copy.initialTargetEndpointRequired);
        return;
      }
      if (inlineEndpoint && (!inlineName.trim() || !inlineBaseUrl.trim())) {
        setFormError(copy.initialTargetInlineEndpointRequired);
        return;
      }
    }
    setSubmitting(true);
    try {
      const payload: ModelConfigCompositeCreate = {
        api_family: apiFamily,
        model_id: modelId.trim(),
        display_name: displayName.trim() || null,
        openai_accepted_format: apiFamily === "openai" ? resolvedAcceptedFormat : null,
        openai_image_operations: apiFamily === "openai" ? resolvedImageOperations : null,
        loadbalance_strategy_id: strategyId,
        is_enabled: !configureLater,
      };
      if (!configureLater) {
        payload.initial_terminal_target = {
          ...(inlineEndpoint
            ? { endpoint_create: { name: inlineName.trim(), base_url: inlineBaseUrl.trim(), api_key: inlineApiKey } }
            : { endpoint_id: endpointId ?? undefined }),
          name: targetName.trim() || null,
          is_active: true,
          openai_text_capability: capability,
        };
      }
      const created = await modelsApi.create(payload);
      onCreated(toListItemLike(created.model));
      onClose();
    } catch (error) {
      setFormError(error instanceof Error ? error.message : messages.modelsData.saveFailed);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !submitting && onClose()}>
      <DialogContent data-testid="create-model-dialog" aria-busy={submitting}>
        <DialogHeader>
          <DialogTitle>{copy.createModelTitle}</DialogTitle>
          <DialogDescription>{copy.createModelDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <OperatorInsetPanel>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="create-model-family">{copy.apiFamilyLabel}</Label>
                <ApiFamilySelect value={apiFamily} onValueChange={(value) => setApiFamily(value as "openai" | "anthropic" | "gemini")} />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="create-model-id">{copy.modelIdLabel}</Label>
                <Input
                  id="create-model-id"
                  value={modelId}
                  onChange={(event) => {
                    const nextModelId = event.target.value;
                    setModelId(nextModelId);
                    setDisplayName((current) => current.trim() === "" || current === modelId ? nextModelId : current);
                  }}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="create-model-display">{copy.displayNameLabel}</Label>
                <Input id="create-model-display" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
              </div>
              {apiFamily === "openai" ? (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="create-model-format">{copy.openaiAcceptedFormatLabel}</Label>
                  <Select value={acceptedFormat} onValueChange={(value) => setAcceptedFormat(value as OpenAICapabilitySelectValue<OpenAIAcceptedFormat>)}>
                    <SelectTrigger id="create-model-format" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {OPENAI_ACCEPTED_FORMAT_SELECT_VALUES.map((format) => (
                        <SelectItem key={format} value={format}>
                          {getOpenAIAcceptedFormatOptionLabel(format, copy)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
              {apiFamily === "openai" ? (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="create-model-image-operations">{copy.openaiImageOperations}</Label>
                  <Select value={imageOperations} onValueChange={(value) => setImageOperations(value as OpenAICapabilitySelectValue<OpenAIImageOperations>)}>
                    <SelectTrigger id="create-model-image-operations" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {OPENAI_IMAGE_OPERATIONS_SELECT_VALUES.map((operations) => (
                        <SelectItem key={operations} value={operations}>
                          {getOpenAIImageOperationsOptionLabel(operations, copy)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <Label htmlFor="create-model-strategy">{copy.loadbalanceStrategyLabel}</Label>
                {loadbalanceStrategies.length === 0 ? (
                  <div className="flex flex-col items-start gap-2">
                    <p className="text-sm text-muted-foreground">{messages.modelDetail.noLoadbalanceStrategiesAvailable}</p>
                    {onCreateLoadbalanceStrategyDefaults ? (
                      <Button type="button" disabled={createLoadbalanceStrategyDefaultsPending} onClick={() => { void onCreateLoadbalanceStrategyDefaults() }}>
                        {createLoadbalanceStrategyDefaultsPending ? <span className="animate-pulse">…</span> : null}
                        {messages.loadbalanceStrategiesTable.createDefaults}
                      </Button>
                    ) : null}
                  </div>
                ) : (
                  <Select
                    value={strategyId === null ? "" : String(strategyId)}
                    onValueChange={(value) => setStrategyId(value === "" ? null : Number(value))}
                  >
                    <SelectTrigger id="create-model-strategy" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {loadbalanceStrategies.map((strategy) => (
                        <SelectItem key={strategy.id} value={String(strategy.id)}>
                          {strategy.name} · {getLoadbalanceStrategyTypeLabel(strategy, messages.loadbalanceStrategyCopy)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>
            </div>
          </OperatorInsetPanel>

          <OperatorSwitchField
            checked={configureLater}
            onCheckedChange={setConfigureLater}
            label={copy.configureLaterLabel}
            description={copy.configureLaterDescription}
          />

          {!configureLater ? (
            <OperatorInsetPanel data-testid="initial-terminal-target-section">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-medium">{copy.initialTargetTitle}</h3>
                <Button variant="outline" size="sm" onClick={() => setInlineEndpoint((current) => !current)} type="button">
                  {inlineEndpoint ? copy.initialTargetUseExisting : copy.initialTargetCreateInline}
                </Button>
              </div>
              {apiFamily === "openai" && capability ? (
                <div className="mt-2 text-xs text-muted-foreground">
                  {copy.ownerDerivedCapability}: {getOpenAIAcceptedFormatOptionLabel(capability, copy)}（{copy.ownerDerivedReadOnly}）
                </div>
              ) : null}
              {inlineEndpoint ? (
                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="create-target-inline-name">{copy.endpointNameLabel}</Label>
                    <Input id="create-target-inline-name" value={inlineName} onChange={(event) => setInlineName(event.target.value)} />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="create-target-inline-url">{copy.endpointBaseUrlLabel}</Label>
                    <Input id="create-target-inline-url" value={inlineBaseUrl} onChange={(event) => setInlineBaseUrl(event.target.value)} />
                  </div>
                  <div className="flex flex-col gap-1.5 sm:col-span-2">
                    <Label htmlFor="create-target-inline-key">{copy.endpointApiKeyLabel}</Label>
                    <Input id="create-target-inline-key" type="password" value={inlineApiKey} onChange={(event) => setInlineApiKey(event.target.value)} />
                  </div>
                </div>
              ) : (
                <div className="mt-3 flex flex-col gap-1.5">
                  <Label htmlFor="create-target-endpoint">{copy.endpointLabel}</Label>
                  <Select
                    value={endpointId === null ? "" : String(endpointId)}
                    onValueChange={(value) => setEndpointId(value === "" ? null : Number(value))}
                  >
                    <SelectTrigger id="create-target-endpoint" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {endpoints.map((endpoint) => (
                        <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                          {endpoint.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
              <div className="mt-3 flex flex-col gap-1.5">
                <Label htmlFor="create-target-name">{copy.targetNameLabel}</Label>
                <Input id="create-target-name" value={targetName} onChange={(event) => setTargetName(event.target.value)} />
              </div>
            </OperatorInsetPanel>
          ) : null}

          {formError ? (
            <OperatorCallout intent="danger">{formError}</OperatorCallout>
          ) : null}

        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={submitting}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button onClick={() => void handleSubmit()} disabled={submitting || strategyById.size === 0}>
            {submitting ? "保存中…" : configureLater ? copy.createDisabledModel : copy.createAndEnable}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}



function toListItemLike(model: ModelConfigListItem | { id: number; model_id: string; display_name: string | null; api_family: string }): ModelConfigListItem {
  return model as ModelConfigListItem;
}
