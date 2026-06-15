import { Plus } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
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
      <Field>
        <FieldLabel>{copy.model}</FieldLabel>
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
            <SelectGroup>
              {nativeModels.map((model) => (
                <SelectItem key={model.id} value={model.model_id}>
                  {model.display_name || model.model_id}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>

      <Field>
        <FieldLabel>{copy.endpoint}</FieldLabel>
        <Select
          value={mappingEndpointId}
          onValueChange={setMappingEndpointId}
          disabled={!mappingModelId || mappingLoading}
        >
          <SelectTrigger>
            <SelectValue placeholder={mappingLoading ? copy.loadingEndpoints : copy.selectEndpoint} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {mappingEndpointOptions.map((endpoint) => (
                <SelectItem key={endpoint.endpointId} value={String(endpoint.endpointId)}>
                  #{endpoint.endpointId} {endpoint.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>

      <Field data-invalid={Boolean(addMappingFxError)}>
        <FieldLabel htmlFor="mapping-fx-rate">{copy.fxRate}</FieldLabel>
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
        {addMappingFxError ? <FieldDescription className="text-destructive">{addMappingFxError}</FieldDescription> : null}
      </Field>

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
          <Plus data-icon="inline-start" />
          {copy.addMapping}
        </Button>
      </div>
    </div>
  );
}
