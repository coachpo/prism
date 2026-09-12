import { modelStrategyLabel } from "./modelStrategyLabel";
import { useEffect, useMemo, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { Button } from "@/components/ui/button";
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { FieldError } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { endpoints as endpointsApi } from "@/lib/api/endpoints";
import { models as modelsApi } from "@/lib/api/models";
import type { Endpoint, LoadbalanceStrategy, ModelConfig, OpenAIAcceptedFormat, OpenAIImageOperations } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system";
import { DEFAULT_OPENAI_ACCEPTED_FORMAT } from "@/pages/models/modelFormState";
import type { OpenAICapabilitySelectValue } from "@/features/models/openaiCapabilityOptions";
import { buildCompositeModelCreatePayload } from "./compositeModelCreatePayload";
import { InitialTerminalTargetFields } from "./InitialTerminalTargetFields";
import { useInitialTerminalTargetUpstreamModelId } from "./useInitialTerminalTargetUpstreamModelId";
import { getModelSaveErrorMessage } from "./modelSaveFeedback";
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
  initialEndpointId,
  createLoadbalanceStrategyDefaultsPending = false,
  onCreateLoadbalanceStrategyDefaults,
}: {
  isOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  onClose: () => void;
  onCreated: (model: ModelConfig) => void | Promise<void>;
  initialEndpointId?: number;
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
  const [directRequestEnabled, setDirectRequestEnabled] = useState(true);
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsError, setEndpointsError] = useState(false);
  const [endpointsRead, setEndpointsRead] = useState(0);
  const [endpointId, setEndpointId] = useState<number | null>(null);
  const [inlineEndpoint, setInlineEndpoint] = useState(false);
  const [inlineName, setInlineName] = useState("");
  const [inlineBaseUrl, setInlineBaseUrl] = useState("");
  const [inlineApiKey, setInlineApiKey] = useState("");
  const initialUpstreamModelId = useInitialTerminalTargetUpstreamModelId({ modelId });
  const [formError, setFormError] = useState<string | null>(null);
  const [invalidField, setInvalidField] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const handleClose = () => {
    initialUpstreamModelId.reset();
    onClose();
  };

  useEffect(() => {
    if (!isOpen) return;
    setFormError(null);
    setInvalidField(null);
    setSubmitting(false);
    setConfigureLater(false);
    setEndpointId(initialEndpointId ?? null);
    setInlineEndpoint(false);
    setApiFamily("openai");
    setModelId("");
    setDisplayName("");
    setAcceptedFormat(DEFAULT_OPENAI_ACCEPTED_FORMAT);
    setImageOperations(OPENAI_CAPABILITY_UNSET);
    setInlineName("");
    setInlineBaseUrl("");
    setInlineApiKey("");
    // 草稿只在一次新的打开会话开始时重置；期间到达的策略列表不得清空操作者的输入。
  }, [isOpen, initialEndpointId]);

  useEffect(() => {
    if (!isOpen) return;
    let current = true;
    setEndpointsLoading(true);
    setEndpointsError(false);
    void endpointsApi.list().then((items) => {
      if (!current) return;
      setEndpoints(items);
      if (initialEndpointId === undefined && items.length === 1) {
        setEndpointId((currentId) => currentId ?? items[0].id);
      }
      if (initialEndpointId !== undefined && !items.some((item) => item.id === initialEndpointId)) {
        setEndpointId(null);
      }
      if (items.length === 0) setInlineEndpoint(true);
    }).catch(() => {
      if (current) setEndpointsError(true);
    }).finally(() => {
      if (current) setEndpointsLoading(false);
    });
    return () => { current = false; };
  }, [isOpen, initialEndpointId, endpointsRead]);

  // 默认路由策略可能在对话框已经打开之后才被创建出来。策略是必填项，所以列表
  // 一到就补上选中值，否则这个必填下拉框会一直空着。按 is_default 这个规范身份
  // 认领，而不是数组下标；操作者已经选过、且仍然存在的策略不被改写。
  useEffect(() => {
    if (!isOpen || loadbalanceStrategies.length === 0) return;
    setStrategyId((current) =>
      current !== null && loadbalanceStrategies.some((strategy) => strategy.id === current)
        ? current
        : (loadbalanceStrategies.find((strategy) => strategy.is_default) ?? loadbalanceStrategies[0]).id,
    );
  }, [isOpen, loadbalanceStrategies]);

  // The dialog can receive the asynchronously-created default strategy while
  // it is open. Reset the entry switch only for a new open session; changing
  // the strategy must not silently undo the operator's choice.
  useEffect(() => {
    if (isOpen) setDirectRequestEnabled(true);
  }, [isOpen]);

  const resolvedAcceptedFormat = fromSelectValue<OpenAIAcceptedFormat>(acceptedFormat) || null;
  const resolvedImageOperations = fromSelectValue<OpenAIImageOperations>(imageOperations) || null;
  const strategyById = useMemo(() => {
    const map = new Map<number, LoadbalanceStrategy>();
    for (const strategy of loadbalanceStrategies) map.set(strategy.id, strategy);
    return map;
  }, [loadbalanceStrategies]);

  const handleSubmit = async () => {
    if (submitting) return;
    setFormError(null);
    setInvalidField(null);
    initialUpstreamModelId.clearError();
    const rejectField = (id: string, message: string) => {
      setInvalidField(id);
      setFormError(message);
      document.getElementById(id)?.focus();
    };
    if (!modelId.trim()) {
      rejectField("create-model-id", messages.modelsData.modelIdRequired);
      return;
    }
    if (apiFamily === "openai" && !resolvedAcceptedFormat && !resolvedImageOperations) {
      rejectField("create-model-format", messages.modelsData.openaiCapabilityRequired);
      return;
    }
    if (strategyId === null) {
      setFormError(copy.selectLoadbalanceStrategy ?? messages.modelsData.selectLoadbalanceStrategy);
      return;
    }
    if (!configureLater) {
      if (!inlineEndpoint && (endpointsLoading || endpointsError)) {
        setFormError(endpointsLoading ? copy.servicesLoading : copy.servicesLoadFailed);
        return;
      }
      if (!initialUpstreamModelId.validate()) {
        document.getElementById("create-target-upstream-model-id")?.focus();
        return;
      }
      if (!inlineEndpoint && endpointId === null) {
        rejectField("create-target-endpoint", copy.initialTargetEndpointRequired);
        return;
      }
      if (inlineEndpoint && (!inlineName.trim() || !inlineBaseUrl.trim())) {
        rejectField(!inlineName.trim() ? "create-target-inline-name" : "create-target-inline-url", copy.initialTargetInlineEndpointRequired);
        return;
      }
      if (inlineEndpoint) {
        try {
          const url = new URL(inlineBaseUrl.trim());
          if (!["http:", "https:"].includes(url.protocol)) throw new Error();
        } catch {
          rejectField("create-target-inline-url", copy.endpointUrlInvalid);
          return;
        }
      }
    }
    setSubmitting(true);
    try {
      const payload = buildCompositeModelCreatePayload({
        apiFamily,
        modelId,
        displayName,
        loadbalanceStrategyId: strategyId,
        configureLater,
        directRequestEnabled,
        openAIAcceptedFormat: resolvedAcceptedFormat,
        openAIImageOperations: resolvedImageOperations,
        initialTerminalTarget: configureLater
          ? undefined
          : {
              ...(inlineEndpoint
                ? { endpoint_create: { name: inlineName.trim(), base_url: inlineBaseUrl.trim(), api_key: inlineApiKey } }
                : { endpoint_id: endpointId ?? undefined }),
              name: null,
              is_active: true,
              upstream_model_id: initialUpstreamModelId.value.trim(),
            },
      });
      const created = await modelsApi.create(payload);
      handleClose();
      await onCreated(created.model);
    } catch (error) {
      if (initialUpstreamModelId.applyServerError(error)) return;
      setFormError(getModelSaveErrorMessage(error));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !submitting && handleClose()}>
      <DialogContent data-testid="create-model-dialog" aria-busy={submitting}>
        <DialogHeader>
          <DialogTitle>{copy.createModelTitle}</DialogTitle>
          <DialogDescription>{copy.createModelDescription}</DialogDescription>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-1 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void handleSubmit();
          }}
        >
        <DialogBody className="flex flex-col gap-4">
          {formError ? (
            <OperatorCallout intent="danger">{formError}</OperatorCallout>
          ) : null}
          <OperatorInsetPanel>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="create-model-family">{copy.apiFamilyLabel}</Label>
                <ApiFamilySelect id="create-model-family" showAll={false} value={apiFamily} onValueChange={(value) => setApiFamily(value as "openai" | "anthropic" | "gemini")} />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="create-model-id" required>
                  {copy.modelIdLabel}
                </Label>
                <Input
                  id="create-model-id"
                  aria-required="true"
                  aria-invalid={invalidField === "create-model-id"}
                  aria-describedby="create-model-id-help"
                  className="font-mono"
                  value={modelId}
                  onChange={(event) => {
                    const nextModelId = event.target.value;
                    setModelId(nextModelId);
                    setInvalidField(null);
                    setDisplayName((current) => current.trim() === "" || current === modelId ? nextModelId : current);
                  }}
                />
                <p id="create-model-id-help" className="text-xs text-muted-foreground">{copy.clientModelNameHint}</p>
                {invalidField === "create-model-id" ? <FieldError>{formError}</FieldError> : null}
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
                  <p className="text-xs text-muted-foreground">{copy.capabilitiesHint}</p>
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
                <Label htmlFor="create-model-strategy" required>
                  {copy.loadbalanceStrategyLabel}
                </Label>
                <p className="text-xs text-muted-foreground">{copy.createStrategyHint}</p>
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
                    // 受控值刚被程序改过、原生 option 还没登记完时，Radix 的
                    // 表单镜像 select 会把一个空值冒泡回来。必填的单选下拉只
                    // 接受目录内的策略，空值与目录外的值一律丢弃，否则刚补上
                    // 的默认策略会被立刻清空。
                    onValueChange={(value) => {
                      const selected = loadbalanceStrategies.find((strategy) => String(strategy.id) === value);
                      if (!selected) return;
                      setStrategyId(selected.id);
                    }}
                  >
                    <SelectTrigger id="create-model-strategy" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {loadbalanceStrategies.map((strategy) => (
                        <SelectItem key={strategy.id} value={String(strategy.id)}>
                          {modelStrategyLabel(strategy)}
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

          <OperatorSwitchField
            checked={directRequestEnabled}
            onCheckedChange={setDirectRequestEnabled}
            label={copy.directRequestEnabled}
            description={copy.directRequestEnabledDescription}
          />

          {!configureLater ? (
            <InitialTerminalTargetFields
              endpointId={endpointId}
              endpoints={endpoints}
              inlineApiKey={inlineApiKey}
              inlineBaseUrl={inlineBaseUrl}
              inlineEndpoint={inlineEndpoint}
              inlineName={inlineName}
              modelId={modelId}
              setEndpointId={setEndpointId}
              setInlineApiKey={setInlineApiKey}
              setInlineBaseUrl={setInlineBaseUrl}
              setInlineEndpoint={setInlineEndpoint}
              setInlineName={setInlineName}
              endpointsLoading={endpointsLoading}
              endpointsError={endpointsError}
              onRetryEndpoints={() => setEndpointsRead((value) => value + 1)}
              invalidField={invalidField}
              fieldError={formError}
              upstreamModelId={initialUpstreamModelId.value}
              upstreamModelIdError={initialUpstreamModelId.error}
              onUpstreamModelIdChange={initialUpstreamModelId.updateFromOperator}
            />
          ) : null}

          <p className="text-xs text-muted-foreground">{configureLater ? copy.configureLaterDescription : copy.createEffect}</p>

        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleClose} disabled={submitting}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button type="submit" disabled={submitting || strategyById.size === 0}>
            {submitting
              ? messages.common.saving
              : configureLater
                ? copy.createDisabledModel
                : copy.createAndEnable}
          </Button>
        </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
