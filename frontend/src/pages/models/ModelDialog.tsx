import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { Loader2, Sparkles } from "lucide-react";
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
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { LoadbalanceStrategy, ModelConfig, ModelConfigListItem, OpenAIAcceptedFormat, OpenAIImageOperations } from "@/lib/types";
import {
  OPENAI_ACCEPTED_FORMAT_SELECT_VALUES,
  OPENAI_IMAGE_OPERATIONS_SELECT_VALUES,
  fromSelectValue,
  getOpenAIAcceptedFormatOptionLabel,
  getOpenAIImageOperationsOptionLabel,
  toSelectValue,
} from "@/features/models/openaiCapabilityOptions";
import { getLoadbalanceStrategyTypeLabel } from "@/lib/loadbalanceRoutingPolicy";
import { OperatorCallout, OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system";
import type { ModelFormData, SubmitEventLike } from "./modelFormState";
import {
  setApiFamilyOnForm,
  setDisplayNameOnForm,
  setModelIdOnForm,
  setOpenAIAcceptedFormatOnForm,
  setOpenAIImageOperationsOnForm,
} from "./modelFormState";

type EditableModel = ModelConfig | ModelConfigListItem;

type Props = {
  editingModel: EditableModel | null;
  formData: ModelFormData;
  formError: string | null;
  isDialogOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  dialogDescription?: string;
  dialogTitle?: string;
  showModelIdInEditMode?: boolean;
  submitLabel?: string;
  createLoadbalanceStrategyDefaultsPending?: boolean;
  setFormData: (value: ModelFormData | ((prev: ModelFormData) => ModelFormData)) => void;
  setIsDialogOpen: (open: boolean) => void;
  setLoadbalanceStrategyId: (value: number | null) => void;
  onCreateLoadbalanceStrategyDefaults?: () => Promise<void>;
  onSubmit: (event: SubmitEventLike) => void;
};



export function ModelDialog({
  editingModel,
  formData,
  formError,
  isDialogOpen,
  loadbalanceStrategies,
  dialogDescription: dialogDescriptionOverride,
  dialogTitle,
  showModelIdInEditMode = false,
  submitLabel,
  createLoadbalanceStrategyDefaultsPending = false,
  setFormData,
  setIsDialogOpen,
  setLoadbalanceStrategyId,
  onCreateLoadbalanceStrategyDefaults,
  onSubmit,
}: Props) {
  const { messages } = useLocale();
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const loadbalanceTableCopy = messages.loadbalanceStrategiesTable;

  const getStrategyTypeLabel = (strategy: LoadbalanceStrategy) => getLoadbalanceStrategyTypeLabel(strategy, strategyCopy);
  const getStrategyOptionText = (strategy: LoadbalanceStrategy) => `${strategy.name} (${getStrategyTypeLabel(strategy)})`;
  const defaultDialogDescription = editingModel ? detailCopy.modelSettingsDescription : copy.newModelDescription;
  const dialogDescription = dialogDescriptionOverride ?? defaultDialogDescription;
  const resolvedDialogTitle = dialogTitle ?? (editingModel ? copy.editModel : messages.modelsPage.newModel);
  const resolvedSubmitLabel = submitLabel ?? copy.save;
  const loadbalanceStrategyValue = String(formData.loadbalance_strategy_id ?? "");
  const selectedLoadbalanceStrategy = [...loadbalanceStrategies]
    .reverse()
    .find((strategy) => strategy.id === formData.loadbalance_strategy_id);
  // Single-strategy truncation warning (SPEC §6.3 / Model SPEC §7.4): two or
  // more enabled mixed targets with a single strategy means every target after
  // the first is never tried for failover.
  const enabledTargetCount = (editingModel?.access_targets ?? []).filter((target) => target.is_enabled).length;
  const singleTruncationWarningVisible =
    selectedLoadbalanceStrategy?.legacy_strategy_type === "single" && enabledTargetCount >= 2;
  const enabledDescription = editingModel
    ? copy.editModelEnabledDescription
    : copy.newModelEnabledDescription;
  const saveDisabled = !selectedLoadbalanceStrategy;
  // Both dimensions are optional on their own; the form validator requires at
  // least one of them for an OpenAI model.
  const openAIAcceptedFormatValue = toSelectValue<OpenAIAcceptedFormat>(formData.openai_accepted_format);
  const openAIImageOperationsValue = toSelectValue<OpenAIImageOperations>(formData.openai_image_operations);

  return (
    <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
      <DialogContent className="max-h-[90vh] overflow-hidden sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{resolvedDialogTitle}</DialogTitle>
          <DialogDescription>{dialogDescription}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col gap-5" autoComplete="off" noValidate>
          <input type="hidden" name="api_family" value={formData.api_family ?? ""} />
          <input type="hidden" name="loadbalance_strategy_id" value={loadbalanceStrategyValue} />
          <input type="hidden" name="is_enabled" value={String(formData.is_enabled)} />
          <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
            {formError ? (
              <OperatorCallout intent="danger" description={formError} />
            ) : null}
            <OperatorInsetPanel>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="min-w-0 flex flex-col gap-2">
                  <Label>{fieldCopy.apiFamily}</Label>
                  <ApiFamilySelect
                    value={formData.api_family ?? ""}
                    onValueChange={(value) => setFormData((prev) => setApiFamilyOnForm(prev, value as typeof prev.api_family))}
                    showAll={false}
                    className="w-full"
                    placeholder={detailCopy.selectApiFamily}
                  />
                </div>
                {formData.api_family === "openai" ? (
                  <div className="min-w-0 flex flex-col gap-2">
                    <Label htmlFor="model-openai-accepted-format">{copy.openaiAcceptedFormat}</Label>
                    <Select
                      value={openAIAcceptedFormatValue}
                      onValueChange={(value) =>
                        setFormData((prev) => setOpenAIAcceptedFormatOnForm(prev, fromSelectValue<OpenAIAcceptedFormat>(value)))}
                    >
                      <SelectTrigger id="model-openai-accepted-format" className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                        <SelectValue>
                            <span className="flex min-w-0 flex-col items-start gap-1 whitespace-normal leading-5">
                              <span>{getOpenAIAcceptedFormatOptionLabel(openAIAcceptedFormatValue, copy)}</span>
                            </span>
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                        <SelectGroup>
                          {OPENAI_ACCEPTED_FORMAT_SELECT_VALUES.map((format) => (
                            <SelectItem key={format} value={format}>
                              <span className="block whitespace-normal break-words pr-4 leading-5">
                                <span className="block">{getOpenAIAcceptedFormatOptionLabel(format, copy)}</span>
                              </span>
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
                {formData.api_family === "openai" ? (
                  <div className="min-w-0 flex flex-col gap-2">
                    <Label htmlFor="model-openai-image-operations">{copy.openaiImageOperations}</Label>
                    <Select
                      value={openAIImageOperationsValue}
                      onValueChange={(value) =>
                        setFormData((prev) => setOpenAIImageOperationsOnForm(prev, fromSelectValue<OpenAIImageOperations>(value)))}
                    >
                      <SelectTrigger id="model-openai-image-operations" className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                        <SelectValue>
                            <span className="flex min-w-0 flex-col items-start gap-1 whitespace-normal leading-5">
                              <span>{getOpenAIImageOperationsOptionLabel(openAIImageOperationsValue, copy)}</span>
                            </span>
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                        <SelectGroup>
                          {OPENAI_IMAGE_OPERATIONS_SELECT_VALUES.map((operations) => (
                            <SelectItem key={operations} value={operations}>
                              <span className="block whitespace-normal break-words pr-4 leading-5">
                                <span className="block">{getOpenAIImageOperationsOptionLabel(operations, copy)}</span>
                              </span>
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
              </div>

              {!editingModel || showModelIdInEditMode ? (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="model-id">{copy.modelId}</Label>
                  <Input
                    id="model-id"
                    name="model_id"
                    autoComplete="off"
                    value={formData.model_id}
                    onChange={(e) => setFormData((prev) => setModelIdOnForm(prev, e.target.value))}
                    placeholder={copy.modelIdPlaceholder}
                    required
                  />
                </div>
              ) : null}

              <div className="flex flex-col gap-2">
                <Label htmlFor="model-display-name">{copy.displayNameOptional}</Label>
                <Input
                  id="model-display-name"
                  name="display_name"
                  autoComplete="off"
                  value={formData.display_name ?? ""}
                  onChange={(e) => setFormData((prev) => setDisplayNameOnForm(prev, e.target.value))}
                  placeholder={copy.optionalFriendlyName}
                />
              </div>
            </OperatorInsetPanel>

            <OperatorInsetPanel className="bg-panel">
              <OperatorInsetPanel>
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{detailCopy.loadbalanceStrategy}</p>
                  <p className="text-sm text-muted-foreground">{copy.routingTypeDescription}</p>
                </div>
                {loadbalanceStrategies.length === 0 ? (
                  <div className="flex flex-col items-start gap-3">
                    <p className="text-sm text-muted-foreground">{detailCopy.noLoadbalanceStrategiesAvailable}</p>
                    {!editingModel && onCreateLoadbalanceStrategyDefaults ? (
                      <Button type="button" disabled={createLoadbalanceStrategyDefaultsPending} onClick={() => { void onCreateLoadbalanceStrategyDefaults(); }}>
                        {createLoadbalanceStrategyDefaultsPending ? (
                          <Loader2 data-icon="inline-start" className="animate-spin" />
                        ) : (
                          <Sparkles data-icon="inline-start" />
                        )}
                        {loadbalanceTableCopy.createDefaults}
                      </Button>
                    ) : null}
                  </div>
                ) : (
                  <Select value={loadbalanceStrategyValue} onValueChange={(value) => setLoadbalanceStrategyId(Number.parseInt(value, 10))}>
                    <SelectTrigger id={editingModel ? "model-loadbalance-strategy" : "create-model-strategy"} className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                      <SelectValue placeholder={detailCopy.selectStrategy}>
                        {selectedLoadbalanceStrategy ? <span className="min-w-0 whitespace-normal break-words leading-5">{getStrategyOptionText(selectedLoadbalanceStrategy)}</span> : null}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                      <SelectGroup>
                        {loadbalanceStrategies.map((strategy) => (
                          <SelectItem key={strategy.id} value={String(strategy.id)}>
                            <span className="block whitespace-normal break-words pr-4 leading-5">{getStrategyOptionText(strategy)}</span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                )}
              </OperatorInsetPanel>

              {singleTruncationWarningVisible ? (
                <OperatorCallout intent="warning" title={strategyCopy.singleTruncationWarning(enabledTargetCount - 1)}>
                  {strategyCopy.singleSummary}
                </OperatorCallout>
              ) : null}

              <OperatorSwitchField
                label={detailCopy.enabled}
                description={enabledDescription}
                checked={formData.is_enabled}
                onCheckedChange={(checked) => setFormData((prev) => ({ ...prev, is_enabled: checked }))}
                className="border-border bg-inset"
              />
              <OperatorSwitchField
                label={copy.directRequestEnabled}
                description={copy.directRequestEnabledDescription}
                checked={formData.direct_request_enabled}
                onCheckedChange={(checked) => setFormData((prev) => ({ ...prev, direct_request_enabled: checked }))}
                className="border-border bg-inset"
              />
            </OperatorInsetPanel>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={() => setIsDialogOpen(false)}>{messages.settingsDialogs.cancel}</Button>
            <Button type="submit" disabled={saveDisabled}>{resolvedSubmitLabel}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
