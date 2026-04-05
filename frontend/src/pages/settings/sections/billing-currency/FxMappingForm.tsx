import { Plus } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { ModelConfigListItem } from "@/lib/types";

interface FxMappingFormProps {
  addMappingFxError: string | null;
  handleAddFxMapping: () => void;
  loadMappingConnections: (modelConfigId: number) => Promise<void>;
  mappingEndpointId: string;
  mappingEndpointOptions: { endpointId: number; label: string }[];
  mappingFxRate: string;
  mappingLoading: boolean;
  mappingModelId: string;
  nativeModels: ModelConfigListItem[];
  setMappingEndpointId: (id: string) => void;
  setMappingFxRate: (rate: string) => void;
  setMappingModelId: (id: string) => void;
}

export function FxMappingForm({
  addMappingFxError,
  handleAddFxMapping,
  loadMappingConnections,
  mappingEndpointId,
  mappingEndpointOptions,
  mappingFxRate,
  mappingLoading,
  mappingModelId,
  nativeModels,
  setMappingEndpointId,
  setMappingFxRate,
  setMappingModelId,
}: FxMappingFormProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  return (
    <div className="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_180px_auto]">
      <div className="space-y-2">
        <Label>{copy.model}</Label>
        <Select
          value={mappingModelId}
          onValueChange={(value) => {
            setMappingModelId(value);
            const selectedModel = nativeModels.find((model) => model.model_id === value);
            if (selectedModel) {
              void loadMappingConnections(selectedModel.id);
            }
          }}
        >
          <SelectTrigger>
            <SelectValue placeholder={copy.selectModel} />
          </SelectTrigger>
          <SelectContent>
            {nativeModels.map((model) => (
              <SelectItem key={model.id} value={model.model_id}>
                {model.display_name || model.model_id}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2">
        <Label>{copy.endpoint}</Label>
        <Select
          value={mappingEndpointId}
          onValueChange={setMappingEndpointId}
          disabled={!mappingModelId || mappingLoading}
        >
          <SelectTrigger>
            <SelectValue placeholder={mappingLoading ? copy.loadingEndpoints : copy.selectEndpoint} />
          </SelectTrigger>
          <SelectContent>
            {mappingEndpointOptions.map((endpoint) => (
              <SelectItem key={endpoint.endpointId} value={String(endpoint.endpointId)}>
                #{endpoint.endpointId} {endpoint.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-2">
        <Label htmlFor="mapping-fx-rate">{copy.fxRate}</Label>
        <Input
          id="mapping-fx-rate"
          name="mapping_fx_rate"
          autoComplete="off"
          value={mappingFxRate}
          onChange={(event) => setMappingFxRate(event.target.value)}
          placeholder={copy.fxRatePlaceholder}
          inputMode="decimal"
          aria-invalid={Boolean(addMappingFxError)}
          className={cn(addMappingFxError && "border-destructive")}
        />
        {addMappingFxError ? <p className="text-xs text-destructive">{addMappingFxError}</p> : null}
      </div>

      <div className="flex items-end">
        <Button
          type="button"
          variant="outline"
          className="w-full"
          onClick={handleAddFxMapping}
          disabled={
            !mappingModelId ||
            !mappingEndpointId ||
            !mappingFxRate.trim() ||
            Boolean(addMappingFxError)
          }
        >
          <Plus className="mr-2 h-4 w-4" />
          {copy.addMapping}
        </Button>
      </div>
    </div>
  );
}
