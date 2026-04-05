import { ApiFamilySelect } from "@/components/ApiFamilySelect";
import { SwitchController } from "@/components/SwitchController";
import { VendorSelect } from "@/components/VendorSelect";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type {
  LoadbalanceStrategy,
  ModelConfigListItem,
  Vendor,
} from "@/lib/types";
import { getAdaptiveRoutingObjectiveLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { ModelFormData, SubmitEventLike } from "./modelFormState";
import {
  setDisplayNameOnForm,
  setModelIdOnForm,
} from "./modelFormState";

type Props = {
  editingModel: ModelConfigListItem | null;
  formData: ModelFormData;
  isDialogOpen: boolean;
  loadbalanceStrategies: LoadbalanceStrategy[];
  nativeModelsForApiFamily: ModelConfigListItem[];
  vendors: Vendor[];
  setFormData: (value: ModelFormData | ((prev: ModelFormData) => ModelFormData)) => void;
  setIsDialogOpen: (open: boolean) => void;
  setLoadbalanceStrategyId: (value: number | null) => void;
  setModelType: (value: "native" | "proxy") => void;
  onSubmit: (event: SubmitEventLike) => void;
};

export function ModelDialog({
  editingModel,
  formData,
  isDialogOpen,
  loadbalanceStrategies,
  nativeModelsForApiFamily,
  vendors,
  setFormData,
  setIsDialogOpen,
  setLoadbalanceStrategyId,
  setModelType,
  onSubmit,
}: Props) {
  const { messages } = useLocale();
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;

  const getStrategyTypeLabel = (strategy: LoadbalanceStrategy) =>
    strategy.strategy_type === "adaptive"
      ? `${strategyCopy.adaptiveFamilyLabel} • ${getAdaptiveRoutingObjectiveLabel(strategy.routing_policy.routing_objective, strategyCopy)}`
      : strategy.legacy_strategy_type === "single"
        ? strategyCopy.singleLabel
        : strategy.legacy_strategy_type === "fill-first"
          ? strategyCopy.fillFirstLabel
          : strategyCopy.roundRobinLabel;

  const getStrategyOptionText = (strategy: LoadbalanceStrategy) => {
    return `${strategy.name} (${getStrategyTypeLabel(strategy)})`;
  };

  const loadbalanceStrategyValue = String(formData.loadbalance_strategy_id ?? "");
  const selectedLoadbalanceStrategy = [...loadbalanceStrategies]
    .reverse()
    .find((strategy) => strategy.id === formData.loadbalance_strategy_id);

  return (
    <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{editingModel ? copy.editModel : messages.modelsPage.newModel}</DialogTitle>
          <DialogDescription>{detailCopy.modelSettingsDescription}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4" autoComplete="off">
          <input type="hidden" name="vendor_id" value={String(formData.vendor_id ?? "")} />
          <input type="hidden" name="api_family" value={formData.api_family ?? ""} />
          <input type="hidden" name="model_type" value={formData.model_type} />
          <input
            type="hidden"
            name="loadbalance_strategy_id"
            value={loadbalanceStrategyValue}
          />
          <input type="hidden" name="is_enabled" value={String(formData.is_enabled)} />
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="min-w-0 space-y-2">
              <Label>{fieldCopy.vendor}</Label>
              <VendorSelect
                value={String(formData.vendor_id ?? "")}
                onValueChange={(value) => {
                  const nextVendorId = Number.parseInt(value, 10);
                  setFormData((prev) => ({
                    ...prev,
                    vendor_id: nextVendorId,
                  }));
                }}
                valueType="vendor_id"
                vendors={vendors}
                showAll={false}
                className="w-full"
                placeholder={detailCopy.selectVendor}
              />
            </div>

            <div className="min-w-0 space-y-2">
              <Label>{fieldCopy.apiFamily}</Label>
              <ApiFamilySelect
                value={formData.api_family ?? ""}
                onValueChange={(value) =>
                  setFormData((prev) => ({
                    ...prev,
                    api_family: value as typeof prev.api_family,
                  }))
                }
                showAll={false}
                className="w-full"
                placeholder={detailCopy.selectApiFamily}
              />
            </div>
          </div>

          {!editingModel && (
            <div className="space-y-2">
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
          )}

          <div className="space-y-2">
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

          <div className="space-y-2">
            <Label htmlFor="model-type">{messages.settingsDialogs.type}</Label>
            <Select value={formData.model_type} onValueChange={(v) => setModelType(v as "native" | "proxy")}> 
              <SelectTrigger id="model-type" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="native">{detailCopy.typeNative}</SelectItem>
                <SelectItem value="proxy">{detailCopy.typeProxy}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {formData.model_type === "proxy" && (
            <div className="space-y-2">
              <Label>{detailCopy.proxyTargets}</Label>
              <p className="text-sm text-muted-foreground">
                <span className="block">
                  {copy.proxyTargetsDescriptionPrimary}
                </span>
                <span className="block">
                  {copy.proxyTargetsDescriptionSecondary}
                </span>
              </p>
              <p className="rounded-md border border-dashed border-border px-3 py-2 text-sm text-muted-foreground">
                {copy.noProxyTargetsSelected}
              </p>
              {nativeModelsForApiFamily.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {copy.noNativeModelsForFamily(formData.api_family || messages.common.apiFamily)}
                </p>
              ) : null}
            </div>
          )}

          {formData.model_type === "native" && (
            <div className="space-y-2">
              <Label htmlFor="model-loadbalance-strategy">{detailCopy.loadbalanceStrategy}</Label>
              {loadbalanceStrategies.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {detailCopy.noLoadbalanceStrategiesAvailable}
                </p>
              ) : (
                <Select
                  value={loadbalanceStrategyValue}
                  onValueChange={(value) => setLoadbalanceStrategyId(Number.parseInt(value, 10))}
                >
                  <SelectTrigger
                    id="model-loadbalance-strategy"
                    className="h-auto w-full min-w-0 items-start py-2 text-left whitespace-normal [&_[data-slot=select-value]]:min-w-0 [&_[data-slot=select-value]]:line-clamp-none [&_[data-slot=select-value]]:whitespace-normal [&_[data-slot=select-value]]:break-words [&_[data-slot=select-value]]:leading-5"
                  >
                    <SelectValue placeholder={detailCopy.selectStrategy}>
                      {selectedLoadbalanceStrategy ? (
                        <span className="min-w-0 whitespace-normal break-words leading-5">
                          {getStrategyOptionText(selectedLoadbalanceStrategy)}
                        </span>
                      ) : null}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]">
                    {loadbalanceStrategies.map((strategy) => (
                      <SelectItem key={strategy.id} value={String(strategy.id)}>
                        <span className="block whitespace-normal break-words pr-4 leading-5">
                          {getStrategyOptionText(strategy)}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
          )}

          <SwitchController
            label={detailCopy.enabled}
            description={copy.routingTypeDescription}
            checked={formData.is_enabled}
            onCheckedChange={(checked) => setFormData({ ...formData, is_enabled: checked })}
          />

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setIsDialogOpen(false)}>{messages.settingsDialogs.cancel}</Button>
            <Button type="submit">{copy.save}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
