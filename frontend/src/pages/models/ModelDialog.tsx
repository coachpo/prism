import type { ReactNode } from "react";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { SwitchController } from "@/components/SwitchController";
import { VendorSelect } from "@/components/VendorSelect";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { LoadbalanceStrategy, ModelConfigListItem, Vendor } from "@/lib/types";
import { getLoadbalanceStrategyTypeLabel } from "@/lib/loadbalanceRoutingPolicy";
import { AccessTargetsEditor } from "./AccessTargetsEditor";
import type { ModelFormData, SubmitEventLike } from "./modelFormState";
import { setApiFamilyOnForm, setDisplayNameOnForm, setModelIdOnForm } from "./modelFormState";

type Props = {
  editingModel: ModelConfigListItem | null;
  formData: ModelFormData;
  formError: string | null;
  isDialogOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  targetModelsForApiFamily: ModelConfigListItem[];
  vendors: Vendor[];
  setFormData: (value: ModelFormData | ((prev: ModelFormData) => ModelFormData)) => void;
  setIsDialogOpen: (open: boolean) => void;
  setLoadbalanceStrategyId: (value: number | null) => void;
  onSubmit: (event: SubmitEventLike) => void;
};

type CapabilityFieldProps = {
  children: ReactNode;
  description?: string;
  error?: string | null;
  id: string;
  label: string;
};

function CapabilityField({ children, description, error, id, label }: CapabilityFieldProps) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {description ? (
        <p className={cn("text-xs", error ? "text-destructive" : "text-muted-foreground")}>
          {error ?? description}
        </p>
      ) : error ? <p className="text-xs text-destructive">{error}</p> : null}
    </div>
  );
}

export function ModelDialog({
  editingModel,
  formData,
  formError,
  isDialogOpen,
  loadbalanceStrategies,
  targetModelsForApiFamily,
  vendors,
  setFormData,
  setIsDialogOpen,
  setLoadbalanceStrategyId,
  onSubmit,
}: Props) {
  const { messages } = useLocale();
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;

  const getStrategyTypeLabel = (strategy: LoadbalanceStrategy) => getLoadbalanceStrategyTypeLabel(strategy, strategyCopy);
  const getStrategyOptionText = (strategy: LoadbalanceStrategy) => `${strategy.name} (${getStrategyTypeLabel(strategy)})`;
  const loadbalanceStrategyValue = String(formData.loadbalance_strategy_id ?? "");
  const selectedLoadbalanceStrategy = [...loadbalanceStrategies]
    .reverse()
    .find((strategy) => strategy.id === formData.loadbalance_strategy_id);
  const enabledDescription = editingModel
    ? copy.editModelEnabledDescription
    : copy.newModelEnabledDescription;
  const saveDisabled = loadbalanceStrategies.length === 0;
  const contextWindowTokensError = formError === messages.modelsData.contextWindowTokensInvalid
    ? formError
    : null;
  const defaultOutputTokenReserveError = formError === messages.modelsData.defaultOutputTokenReserveInvalid
    ? formError
    : null;
  const maxContextUtilizationError = formError === messages.modelsData.maxContextUtilizationInvalid
    ? formError
    : null;
  const preferredContextUtilizationThresholdError =
    formError === messages.modelsData.preferredContextUtilizationThresholdInvalid
      || formError === messages.modelsData.preferredContextUtilizationThresholdExceedsMaxContextUtilization
      ? formError
      : null;
  const hasCapabilityValidationError = Boolean(
    contextWindowTokensError
      || defaultOutputTokenReserveError
      || maxContextUtilizationError
      || preferredContextUtilizationThresholdError,
  );
  const accessTargetsError = hasCapabilityValidationError ? null : formError;

  return (
    <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
      <DialogContent className="max-h-[90vh] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editingModel ? copy.editModel : messages.modelsPage.newModel}</DialogTitle>
          <DialogDescription>{detailCopy.modelSettingsDescription}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="flex min-h-0 flex-col gap-5" autoComplete="off" noValidate>
          <input type="hidden" name="vendor_id" value={String(formData.vendor_id ?? "")} />
          <input type="hidden" name="api_family" value={formData.api_family ?? ""} />
          <input type="hidden" name="loadbalance_strategy_id" value={loadbalanceStrategyValue} />
          <input type="hidden" name="is_enabled" value={String(formData.is_enabled)} />
          <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
            {formError && !hasCapabilityValidationError ? (
              <div className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {formError}
              </div>
            ) : null}
            <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="min-w-0 flex flex-col gap-2">
                  <Label>{fieldCopy.vendor}</Label>
                  <VendorSelect
                    value={String(formData.vendor_id ?? "")}
                    onValueChange={(value) => setFormData((prev) => ({ ...prev, vendor_id: value ? Number.parseInt(value, 10) : null }))}
                    allowEmpty={true}
                    valueType="vendor_id"
                    vendors={vendors}
                    showAll={false}
                    className="w-full"
                    placeholder={detailCopy.selectVendor}
                  />
                </div>

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
              </div>

              {!editingModel ? (
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
            </div>

            <div className="flex flex-col gap-4 rounded-lg border bg-muted/15 p-4">
              <div className="flex flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{copy.contextRoutingDefaults}</p>
              </div>
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <CapabilityField
                  id="model-context-window-tokens"
                  label={copy.contextWindowTokens}
                  description={copy.contextWindowTokensHelper}
                  error={contextWindowTokensError}
                >
                  <Input
                    id="model-context-window-tokens"
                    name="context_window_tokens"
                    type="number"
                    min="1"
                    step="1"
                    value={formData.context_window_tokens}
                    onChange={(event) => setFormData((prev) => ({ ...prev, context_window_tokens: event.target.value }))}
                    aria-invalid={Boolean(contextWindowTokensError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="model-default-output-token-reserve"
                  label={copy.defaultOutputTokenReserve}
                  error={defaultOutputTokenReserveError}
                >
                  <Input
                    id="model-default-output-token-reserve"
                    name="default_output_token_reserve"
                    type="number"
                    min="1"
                    step="1"
                    value={formData.default_output_token_reserve}
                    onChange={(event) => setFormData((prev) => ({ ...prev, default_output_token_reserve: event.target.value }))}
                    aria-invalid={Boolean(defaultOutputTokenReserveError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="model-max-context-utilization"
                  label={copy.maxContextUtilization}
                  error={maxContextUtilizationError}
                >
                  <Input
                    id="model-max-context-utilization"
                    name="max_context_utilization"
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={formData.max_context_utilization}
                    onChange={(event) => setFormData((prev) => ({ ...prev, max_context_utilization: event.target.value }))}
                    aria-invalid={Boolean(maxContextUtilizationError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="model-preferred-context-utilization-threshold"
                  label={copy.preferredContextUtilizationThreshold}
                  description={copy.preferredContextUtilizationThresholdHelper}
                  error={preferredContextUtilizationThresholdError}
                >
                  <Input
                    id="model-preferred-context-utilization-threshold"
                    name="preferred_context_utilization_threshold"
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={formData.preferred_context_utilization_threshold}
                    onChange={(event) => setFormData((prev) => ({ ...prev, preferred_context_utilization_threshold: event.target.value }))}
                    aria-invalid={Boolean(preferredContextUtilizationThresholdError) || undefined}
                  />
                </CapabilityField>
              </div>
            </div>

            <div className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="flex flex-col gap-3 rounded-lg border bg-muted/15 p-4">
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{detailCopy.loadbalanceStrategy}</p>
                  <p className="text-sm text-muted-foreground">{copy.routingTypeDescription}</p>
                </div>
                {loadbalanceStrategies.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{detailCopy.noLoadbalanceStrategiesAvailable}</p>
                ) : (
                  <Select value={loadbalanceStrategyValue} onValueChange={(value) => setLoadbalanceStrategyId(Number.parseInt(value, 10))}>
                    <SelectTrigger id="model-loadbalance-strategy" className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                      <SelectValue placeholder={detailCopy.selectStrategy}>
                        {selectedLoadbalanceStrategy ? <span className="min-w-0 whitespace-normal break-words leading-5">{getStrategyOptionText(selectedLoadbalanceStrategy)}</span> : null}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                      {loadbalanceStrategies.map((strategy) => (
                        <SelectItem key={strategy.id} value={String(strategy.id)}>
                          <span className="block whitespace-normal break-words pr-4 leading-5">{getStrategyOptionText(strategy)}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>

              <AccessTargetsEditor
                apiFamilyLabel={formData.api_family}
                accessTargets={formData.access_targets}
                modelOptions={targetModelsForApiFamily}
                error={accessTargetsError}
                onChange={(accessTargets) => setFormData((prev) => ({ ...prev, access_targets: accessTargets }))}
              />

              <SwitchController
                label={detailCopy.enabled}
                description={enabledDescription}
                checked={formData.is_enabled}
                onCheckedChange={(checked) => setFormData((prev) => ({ ...prev, is_enabled: checked }))}
              />
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={() => setIsDialogOpen(false)}>{messages.settingsDialogs.cancel}</Button>
            <Button type="submit" disabled={saveDisabled}>{copy.save}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
