import type { Dispatch, ReactNode, SetStateAction } from "react";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { SwitchController } from "@/components/SwitchController";
import { VendorSelect } from "@/components/VendorSelect";
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
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import { cn } from "@/lib/utils";
import type { LoadbalanceStrategy, ModelConfig, ModelConfigListItem, Vendor } from "@/lib/types";
import { AccessTargetsEditor } from "../models/AccessTargetsEditor";
import {
  setApiFamilyOnForm,
  setDisplayNameOnForm,
  setModelIdOnForm,
  type ModelFormData,
  type SubmitEventLike,
} from "../models/modelFormState";

interface ModelSettingsDialogProps {
  formData: ModelFormData;
  handleEditModelSubmit: (event: SubmitEventLike) => Promise<void>;
  isOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  model: ModelConfig | null;
  targetEditorError: string | null;
  targetModelsForApiFamily: ModelConfigListItem[];
  onOpenChange: (open: boolean) => void;
  setFormData: Dispatch<SetStateAction<ModelFormData>>;
  setLoadbalanceStrategyId: (value: number | null) => void;
  vendors: Vendor[];
}

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

export function ModelSettingsDialog({
  formData,
  handleEditModelSubmit,
  isOpen,
  loadbalanceStrategies,
  model,
  targetEditorError,
  targetModelsForApiFamily,
  onOpenChange,
  setFormData,
  setLoadbalanceStrategyId,
  vendors,
}: ModelSettingsDialogProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const modelsUiCopy = messages.modelsUi;
  const modelsDataCopy = messages.modelsData;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;

  const getStrategyDetailLabel = (strategy: LoadbalanceStrategy) =>
    getLoadbalanceStrategyDetailLabel(strategy, strategyCopy);

  const getStrategyOptionText = (strategy: LoadbalanceStrategy) => {
    return `${strategy.name} (${getStrategyDetailLabel(strategy)})`;
  };

  const loadbalanceStrategyValue = String(formData.loadbalance_strategy_id ?? "");
  const selectedLoadbalanceStrategy = loadbalanceStrategies.find(
    (strategy) => strategy.id === formData.loadbalance_strategy_id,
  );
  const contextWindowTokensError = targetEditorError === modelsDataCopy.contextWindowTokensInvalid
    ? targetEditorError
    : null;
  const defaultOutputTokenReserveError = targetEditorError === modelsDataCopy.defaultOutputTokenReserveInvalid
    ? targetEditorError
    : null;
  const maxContextUtilizationError = targetEditorError === modelsDataCopy.maxContextUtilizationInvalid
    ? targetEditorError
    : null;
  const preferredContextUtilizationThresholdError =
    targetEditorError === modelsDataCopy.preferredContextUtilizationThresholdInvalid
      || targetEditorError === modelsDataCopy.preferredContextUtilizationThresholdExceedsMaxContextUtilization
      ? targetEditorError
      : null;
  const hasCapabilityValidationError = Boolean(
    contextWindowTokensError
      || defaultOutputTokenReserveError
      || maxContextUtilizationError
      || preferredContextUtilizationThresholdError,
  );
  const accessTargetsError = hasCapabilityValidationError ? null : targetEditorError;

  if (!model) {
    return null;
  }

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(92vh,48rem)] max-h-[92vh] max-w-2xl flex-col overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b bg-background px-6 py-5 sm:px-7">
          <DialogTitle>{copy.modelSettingsTitle}</DialogTitle>
          <DialogDescription>{copy.modelSettingsDescription}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleEditModelSubmit} className="flex min-h-0 flex-1 flex-col" noValidate>
          <input type="hidden" name="is_enabled" value={String(formData.is_enabled)} />
          <DialogBody className="min-h-0 flex-1 overflow-y-auto px-6 py-5 sm:px-7" data-testid="model-settings-scroll-body">
            <section
              className="flex flex-col gap-4 rounded-2xl border bg-muted/20 p-4 sm:p-5"
              data-testid="model-settings-basics-section"
            >
              <div className="flex flex-col gap-1">
                <h2 className="text-sm font-semibold tracking-tight text-foreground">{copy.configuration}</h2>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex min-w-0 flex-col gap-2 sm:col-span-2">
                  <Label htmlFor="edit-display-name">{copy.displayName}</Label>
                  <Input
                    id="edit-display-name"
                    name="display_name"
                    autoComplete="off"
                    value={formData.display_name ?? ""}
                    onChange={(event) => setFormData((current) => setDisplayNameOnForm(current, event.target.value))}
                    placeholder={copy.displayNamePlaceholder}
                  />
                </div>

                <div className="flex min-w-0 flex-col gap-2 sm:col-span-2">
                  <Label htmlFor="edit-model-id">{copy.modelIdLabel}</Label>
                  <Input
                    id="edit-model-id"
                    name="model_id"
                    autoComplete="off"
                    value={formData.model_id}
                    onChange={(event) => setFormData((current) => setModelIdOnForm(current, event.target.value))}
                    required
                  />
                </div>

                <div className="flex min-w-0 flex-col gap-2">
                  <Label>{fieldCopy.vendor}</Label>
                  <VendorSelect
                    value={String(formData.vendor_id ?? "")}
                    onValueChange={(value) => {
                      const nextVendorId = value ? Number.parseInt(value, 10) : null;
                      setFormData((current) => ({ ...current, vendor_id: nextVendorId }));
                    }}
                    allowEmpty={true}
                    valueType="vendor_id"
                    vendors={vendors}
                    showAll={false}
                    className="w-full"
                    placeholder={copy.selectVendor}
                  />
                </div>

                <div className="flex min-w-0 flex-col gap-2">
                  <Label>{fieldCopy.apiFamily}</Label>
                  <ApiFamilySelect
                    value={formData.api_family}
                    onValueChange={(value) =>
                      setFormData((current) => setApiFamilyOnForm(current, value as typeof current.api_family))
                    }
                    showAll={false}
                    className="w-full"
                    placeholder={copy.selectApiFamily}
                  />
                </div>
              </div>
            </section>

            <section
              className="flex flex-col gap-4 rounded-2xl border bg-muted/15 p-4 sm:p-5"
              data-testid="model-settings-context-routing-section"
            >
              <div className="flex flex-col gap-1">
                <h2 className="text-sm font-semibold tracking-tight text-foreground">{modelsUiCopy.contextRoutingDefaults}</h2>
                <p className="text-sm text-muted-foreground">{modelsUiCopy.contextRoutingDefaultsDescription}</p>
              </div>

              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <CapabilityField
                  id="edit-context-window-tokens"
                  label={modelsUiCopy.contextWindowTokens}
                  description={modelsUiCopy.contextWindowTokensHelper}
                  error={contextWindowTokensError}
                >
                  <Input
                    id="edit-context-window-tokens"
                    name="context_window_tokens"
                    type="number"
                    min="1"
                    step="1"
                    value={formData.context_window_tokens}
                    onChange={(event) => setFormData((current) => ({ ...current, context_window_tokens: event.target.value }))}
                    aria-invalid={Boolean(contextWindowTokensError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="edit-default-output-token-reserve"
                  label={modelsUiCopy.defaultOutputTokenReserve}
                  error={defaultOutputTokenReserveError}
                >
                  <Input
                    id="edit-default-output-token-reserve"
                    name="default_output_token_reserve"
                    type="number"
                    min="1"
                    step="1"
                    value={formData.default_output_token_reserve}
                    onChange={(event) => setFormData((current) => ({ ...current, default_output_token_reserve: event.target.value }))}
                    aria-invalid={Boolean(defaultOutputTokenReserveError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="edit-max-context-utilization"
                  label={modelsUiCopy.maxContextUtilization}
                  error={maxContextUtilizationError}
                >
                  <Input
                    id="edit-max-context-utilization"
                    name="max_context_utilization"
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={formData.max_context_utilization}
                    onChange={(event) => setFormData((current) => ({ ...current, max_context_utilization: event.target.value }))}
                    aria-invalid={Boolean(maxContextUtilizationError) || undefined}
                  />
                </CapabilityField>

                <CapabilityField
                  id="edit-preferred-context-utilization-threshold"
                  label={modelsUiCopy.preferredContextUtilizationThreshold}
                  description={modelsUiCopy.preferredContextUtilizationThresholdHelper}
                  error={preferredContextUtilizationThresholdError}
                >
                  <Input
                    id="edit-preferred-context-utilization-threshold"
                    name="preferred_context_utilization_threshold"
                    type="number"
                    min="0"
                    max="1"
                    step="0.01"
                    value={formData.preferred_context_utilization_threshold}
                    onChange={(event) => setFormData((current) => ({ ...current, preferred_context_utilization_threshold: event.target.value }))}
                    aria-invalid={Boolean(preferredContextUtilizationThresholdError) || undefined}
                  />
                </CapabilityField>
              </div>
            </section>

            <section
              className="flex flex-col gap-4 rounded-2xl border p-4 sm:p-5"
              data-testid="model-settings-routing-section"
            >
              <div className="flex flex-col gap-1">
                <h2 className="text-sm font-semibold tracking-tight text-foreground">{copy.loadbalanceStrategy}</h2>
                <p className="text-sm text-muted-foreground">{copy.modelConfigurationAndConnectionRouting}</p>
              </div>

              <div className="flex min-w-0 flex-col gap-4">
                <div className="flex min-w-0 flex-col gap-2">
                  {loadbalanceStrategies.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{copy.noLoadbalanceStrategiesAvailable}</p>
                  ) : (
                    <Select value={loadbalanceStrategyValue} onValueChange={(value) => setLoadbalanceStrategyId(Number.parseInt(value, 10))}>
                      <SelectTrigger id="edit-loadbalance-strategy" className="w-full min-w-0 max-w-full">
                        <SelectValue placeholder={copy.selectStrategy}>
                          {selectedLoadbalanceStrategy ? (
                            <span className="block min-w-0 max-w-full truncate">{getStrategyOptionText(selectedLoadbalanceStrategy)}</span>
                          ) : null}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent position="popper" className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                        {loadbalanceStrategies.map((strategy) => (
                          <SelectItem key={strategy.id} value={String(strategy.id)}>{getStrategyOptionText(strategy)}</SelectItem>
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
                  onChange={(accessTargets) => setFormData((current) => ({ ...current, access_targets: accessTargets }))}
                />

                <SwitchController
                  label={copy.enabled}
                  checked={formData.is_enabled}
                  onCheckedChange={(checked) => setFormData((current) => ({ ...current, is_enabled: checked }))}
                />
              </div>
            </section>
          </DialogBody>

          <DialogFooter className="shrink-0 border-t bg-background px-6 py-4 sm:justify-between sm:px-7">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {copy.cancel}
            </Button>
            <Button type="submit">{copy.saveChanges}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
