import { useState } from "react";
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
import type { LoadbalanceStrategy, ModelConfig, Vendor } from "@/lib/types";

interface ModelSettingsDialogProps {
  editLoadbalanceStrategyId: string;
  isOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  onOpenChange: (open: boolean) => void;
  model: ModelConfig | null;
  vendors: Vendor[];
  setEditLoadbalanceStrategyId: (value: string) => void;
  handleEditModelSubmit: (e: React.FormEvent<HTMLFormElement>) => Promise<void>;
}

export function ModelSettingsDialog({
  editLoadbalanceStrategyId,
  isOpen,
  loadbalanceStrategies,
  onOpenChange,
  model,
  vendors,
  setEditLoadbalanceStrategyId,
  handleEditModelSubmit,
}: ModelSettingsDialogProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(92vh,48rem)] max-h-[92vh] max-w-2xl flex-col overflow-hidden p-0 sm:max-w-2xl">
        {model ? (
          <ModelSettingsForm
            key={`${model.id}:${model.updated_at}`}
            editLoadbalanceStrategyId={editLoadbalanceStrategyId}
            copy={copy}
            fieldCopy={fieldCopy}
            handleEditModelSubmit={handleEditModelSubmit}
            loadbalanceStrategies={loadbalanceStrategies}
            model={model}
            onOpenChange={onOpenChange}
            setEditLoadbalanceStrategyId={setEditLoadbalanceStrategyId}
            strategyCopy={strategyCopy}
            vendors={vendors}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

type ModelSettingsFormProps = {
  copy: {
    cancel: string;
    configuration: string;
    displayName: string;
    displayNamePlaceholder: string;
    loadbalanceStrategy: string;
    loadbalanceStrategyLabel: string;
    modelIdLabel: string;
    modelSettingsDescription: string;
    modelSettingsTitle: string;
    noLoadbalanceStrategiesAvailable: string;
    proxyTargets: string;
    proxyTargetsHint: string;
    saveChanges: string;
    selectApiFamily: string;
    selectStrategy: string;
    selectVendor: string;
    targets: (count: string) => string;
  };
  editLoadbalanceStrategyId: string;
  fieldCopy: {
    apiFamily: string;
    vendor: string;
  };
  handleEditModelSubmit: (e: React.FormEvent<HTMLFormElement>) => Promise<void>;
  loadbalanceStrategies: LoadbalanceStrategy[];
  model: ModelConfig;
  onOpenChange: (open: boolean) => void;
  setEditLoadbalanceStrategyId: (value: string) => void;
  strategyCopy: {
    adaptiveFamilyLabel: string;
    fillFirstLabel: string;
    legacyFamilyLabel: string;
    maximizeAvailabilityLabel: string;
    maximizeAvailabilitySummary: string;
    minimizeLatencyLabel: string;
    minimizeLatencySummary: string;
    roundRobinLabel: string;
    singleLabel: string;
  };
  vendors: Vendor[];
};

function ModelSettingsForm({
  copy,
  editLoadbalanceStrategyId,
  fieldCopy,
  handleEditModelSubmit,
  loadbalanceStrategies,
  model,
  onOpenChange,
  setEditLoadbalanceStrategyId,
  strategyCopy,
  vendors,
}: ModelSettingsFormProps) {
  const [selectedVendorId, setSelectedVendorId] = useState(
    String(model.vendor_id ?? model.vendor?.id ?? ""),
  );
  const [selectedApiFamily, setSelectedApiFamily] = useState(model.api_family ?? "openai");

  const handleApiFamilyChange = (value: string) => {
    setSelectedApiFamily(value as "openai" | "anthropic" | "gemini");
  };

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

  const selectedLoadbalanceStrategy = loadbalanceStrategies.find(
    (strategy) => String(strategy.id) === editLoadbalanceStrategyId,
  );

  return (
    <>
      <DialogHeader className="shrink-0 border-b bg-background px-6 py-5 sm:px-7">
        <DialogTitle>{copy.modelSettingsTitle}</DialogTitle>
        <DialogDescription>{copy.modelSettingsDescription}</DialogDescription>
      </DialogHeader>

      <form onSubmit={handleEditModelSubmit} className="flex min-h-0 flex-1 flex-col">
        <input type="hidden" name="vendor_id" value={selectedVendorId} />
        <input type="hidden" name="api_family" value={selectedApiFamily} />

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
                  defaultValue={model.display_name || ""}
                  placeholder={copy.displayNamePlaceholder}
                />
              </div>

              <div className="flex min-w-0 flex-col gap-2 sm:col-span-2">
                <Label htmlFor="edit-model-id">{copy.modelIdLabel}</Label>
                <Input
                  id="edit-model-id"
                  name="model_id"
                  autoComplete="off"
                  defaultValue={model.model_id}
                  required
                />
              </div>

              <div className="flex min-w-0 flex-col gap-2">
                <Label>{fieldCopy.vendor}</Label>
                <VendorSelect
                  value={selectedVendorId}
                  onValueChange={setSelectedVendorId}
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
                  value={selectedApiFamily}
                  onValueChange={handleApiFamilyChange}
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
                {model.model_type === "proxy" ? copy.proxyTargets : copy.loadbalanceStrategy}
              </h2>
              {model.model_type === "proxy" ? (
                <p className="text-sm text-muted-foreground">{copy.proxyTargetsHint}</p>
              ) : null}
            </div>

            {model.model_type === "proxy" ? (
              <div className="grid gap-3 rounded-xl border border-dashed bg-muted/15 p-4 sm:grid-cols-2">
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                    {copy.proxyTargets}
                  </p>
                  <p className="text-sm text-foreground">{copy.targets(String(model.proxy_targets.length))}</p>
                </div>
              </div>
            ) : (
              <div className="flex min-w-0 flex-col gap-4">
                <div className="flex min-w-0 flex-col gap-2">
                  {loadbalanceStrategies.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{copy.noLoadbalanceStrategiesAvailable}</p>
                  ) : (
                    <Select value={editLoadbalanceStrategyId || undefined} onValueChange={setEditLoadbalanceStrategyId}>
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
    </>
  );
}
