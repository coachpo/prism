import { useState } from "react";
import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { VendorSelect } from "@/components/VendorSelect";
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
      <DialogContent>
        {model && (
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
        )}
      </DialogContent>
    </Dialog>
  );
}

type ModelSettingsFormProps = {
  copy: {
    cancel: string;
    displayName: string;
    displayNamePlaceholder: string;
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
  };
  editLoadbalanceStrategyId: string;
  fieldCopy: {
    vendor: string;
    apiFamily: string;
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
  const [selectedApiFamily, setSelectedApiFamily] = useState(
    model.api_family ?? "openai",
  );

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
      <DialogHeader>
        <DialogTitle>{copy.modelSettingsTitle}</DialogTitle>
        <DialogDescription>{copy.modelSettingsDescription}</DialogDescription>
      </DialogHeader>
      <form onSubmit={handleEditModelSubmit} className="min-w-0 space-y-4">
        <input type="hidden" name="vendor_id" value={selectedVendorId} />
        <input type="hidden" name="api_family" value={selectedApiFamily} />

        <div className="space-y-2">
          <Label htmlFor="edit-display-name">{copy.displayName}</Label>
          <Input
            id="edit-display-name"
            name="display_name"
            autoComplete="off"
            defaultValue={model.display_name || ""}
            placeholder={copy.displayNamePlaceholder}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="edit-model-id">{copy.modelIdLabel}</Label>
          <Input id="edit-model-id" name="model_id" autoComplete="off" defaultValue={model.model_id} required />
        </div>
        <div className="grid min-w-0 gap-4 sm:grid-cols-2">
          <div className="min-w-0 space-y-2">
            <Label>{fieldCopy.vendor}</Label>
            <VendorSelect
              value={selectedVendorId}
              onValueChange={setSelectedVendorId}
              valueType="vendor_id"
              vendors={vendors}
              showAll={false}
              className="w-full"
              placeholder={copy.selectVendor}
            />
          </div>
          <div className="min-w-0 space-y-2">
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
        {model.model_type === "proxy" && (
          <div className="space-y-2">
            <Label>{copy.proxyTargets}</Label>
            <p className="text-sm text-muted-foreground">{copy.proxyTargetsHint}</p>
          </div>
        )}
        {model.model_type === "native" && (
          <div className="min-w-0 space-y-2">
            <Label htmlFor="edit-loadbalance-strategy">{copy.loadbalanceStrategyLabel}</Label>
            {loadbalanceStrategies.length === 0 ? (
              <p className="text-sm text-muted-foreground">{copy.noLoadbalanceStrategiesAvailable}</p>
            ) : (
                <Select
                  value={editLoadbalanceStrategyId || undefined}
                  onValueChange={setEditLoadbalanceStrategyId}
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
        )}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {copy.cancel}
          </Button>
          <Button type="submit">{copy.saveChanges}</Button>
        </DialogFooter>
      </form>
    </>
  );
}
