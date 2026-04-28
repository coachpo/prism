import type { Dispatch, SetStateAction } from "react";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
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
import { getAdaptiveRoutingObjectiveLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy, ModelConfig, ModelConfigListItem, Vendor } from "@/lib/types";
import { ProxyTargetsEditor } from "../models/ProxyTargetsEditor";
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
  nativeModelsForApiFamily: ModelConfigListItem[];
  onOpenChange: (open: boolean) => void;
  setFormData: Dispatch<SetStateAction<ModelFormData>>;
  setLoadbalanceStrategyId: (value: number | null) => void;
  vendors: Vendor[];
}

export function ModelSettingsDialog({
  formData,
  handleEditModelSubmit,
  isOpen,
  loadbalanceStrategies,
  model,
  nativeModelsForApiFamily,
  onOpenChange,
  setFormData,
  setLoadbalanceStrategyId,
  vendors,
}: ModelSettingsDialogProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;

  const getStrategyDetailLabel = (strategy: LoadbalanceStrategy) =>
    strategy.strategy_type === "adaptive"
      ? `${strategyCopy.adaptiveFamilyLabel} • ${getAdaptiveRoutingObjectiveLabel(strategy.routing_policy.routing_objective, strategyCopy)}`
      : strategy.legacy_strategy_type === "single"
        ? `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.singleLabel}`
        : strategy.legacy_strategy_type === "fill-first"
          ? `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.fillFirstLabel}`
          : `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.roundRobinLabel}`;

  const getStrategyOptionText = (strategy: LoadbalanceStrategy) => {
    return `${strategy.name} (${getStrategyDetailLabel(strategy)})`;
  };

  const loadbalanceStrategyValue = String(formData.loadbalance_strategy_id ?? "");
  const selectedLoadbalanceStrategy = loadbalanceStrategies.find(
    (strategy) => strategy.id === formData.loadbalance_strategy_id,
  );

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

        <form onSubmit={handleEditModelSubmit} className="flex min-h-0 flex-1 flex-col">
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
              className="flex flex-col gap-4 rounded-2xl border p-4 sm:p-5"
              data-testid="model-settings-routing-section"
            >
              <div className="flex flex-col gap-1">
                <h2 className="text-sm font-semibold tracking-tight text-foreground">
                  {formData.model_type === "proxy" ? copy.proxyTargets : copy.loadbalanceStrategy}
                </h2>
                {formData.model_type === "proxy" ? (
                  <p className="text-sm text-muted-foreground">{copy.proxyTargetsHint}</p>
                ) : null}
              </div>

              {formData.model_type === "proxy" ? (
                <ProxyTargetsEditor
                  apiFamilyLabel={formData.api_family || fieldCopy.apiFamily}
                  availableTargets={nativeModelsForApiFamily.map((candidate) => ({
                    modelId: candidate.model_id,
                    label: candidate.display_name || candidate.model_id,
                  }))}
                  proxyTargets={formData.proxy_targets}
                  onChange={(proxyTargets) =>
                    setFormData((current) => ({ ...current, proxy_targets: proxyTargets }))
                  }
                />
              ) : (
                <div className="flex min-w-0 flex-col gap-4">
                  <div className="flex min-w-0 flex-col gap-2">
                    {loadbalanceStrategies.length === 0 ? (
                      <p className="text-sm text-muted-foreground">{copy.noLoadbalanceStrategiesAvailable}</p>
                    ) : (
                      <Select
                        value={loadbalanceStrategyValue}
                        onValueChange={(value) => setLoadbalanceStrategyId(Number.parseInt(value, 10))}
                      >
                        <SelectTrigger id="edit-loadbalance-strategy" className="w-full min-w-0 max-w-full">
                          <SelectValue placeholder={copy.selectStrategy}>
                            {selectedLoadbalanceStrategy ? (
                              <span className="block min-w-0 max-w-full truncate">
                                {getStrategyOptionText(selectedLoadbalanceStrategy)}
                              </span>
                            ) : null}
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent
                          position="popper"
                          className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]"
                        >
                          {loadbalanceStrategies.map((strategy) => (
                            <SelectItem key={strategy.id} value={String(strategy.id)}>
                              {getStrategyOptionText(strategy)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                  </div>
                </div>
              )}
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
