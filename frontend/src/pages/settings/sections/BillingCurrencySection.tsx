import type { ReactNode } from "react";
import { Coins } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { Connection, CostingSettingsUpdate, EndpointFxMapping, ModelConfigListItem } from "@/lib/types";
import { FxMappingForm } from "./billing-currency/FxMappingForm";
import { FxMappingsSummary } from "./billing-currency/FxMappingsSummary";
import { FxMappingsTable } from "./billing-currency/FxMappingsTable";
import { ReportingCurrencyCard } from "./billing-currency/ReportingCurrencyCard";

interface BillingCurrencySectionProps {
  billingDirty: boolean;
  renderSectionSaveState: (section: "billing" | "timezone", isDirty: boolean) => ReactNode;
  handleSaveCostingSettings: (section: "billing" | "timezone") => Promise<void>;
  costingUnavailable: boolean;
  costingLoading: boolean;
  costingSaving: boolean;
  costingForm: CostingSettingsUpdate;
  setCostingForm: React.Dispatch<React.SetStateAction<CostingSettingsUpdate>>;
  normalizedCurrentCosting: CostingSettingsUpdate;
  nativeModels: ModelConfigListItem[];
  modelLabelMap: Map<string, string>;
  mappingModelId: string;
  setMappingModelId: (id: string) => void;
  loadMappingConnections: (modelConfigId: number) => Promise<void>;
  mappingEndpointId: string;
  setMappingEndpointId: (id: string) => void;
  mappingConnections: Connection[];
  mappingLoading: boolean;
  mappingEndpointOptions: { endpointId: number; label: string }[];
  mappingFxRate: string;
  setMappingFxRate: (rate: string) => void;
  addMappingFxError: string | null;
  handleAddFxMapping: () => void;
  editingMappingKey: string | null;
  editingMappingFxRate: string;
  setEditingMappingFxRate: (rate: string) => void;
  editMappingFxError: string | null;
  handleSaveEditFxMapping: () => void;
  handleCancelEditFxMapping: () => void;
  handleStartEditFxMapping: (mapping: EndpointFxMapping) => void;
  handleDeleteFxMapping: (mapping: EndpointFxMapping) => void;
}

export function BillingCurrencySection({
  billingDirty,
  renderSectionSaveState,
  handleSaveCostingSettings,
  costingUnavailable,
  costingLoading,
  costingSaving,
  costingForm,
  setCostingForm,
  normalizedCurrentCosting,
  nativeModels,
  modelLabelMap,
  mappingModelId,
  setMappingModelId,
  loadMappingConnections,
  mappingEndpointId,
  setMappingEndpointId,
  mappingEndpointOptions,
  mappingLoading,
  mappingFxRate,
  setMappingFxRate,
  addMappingFxError,
  handleAddFxMapping,
  editingMappingKey,
  editingMappingFxRate,
  setEditingMappingFxRate,
  editMappingFxError,
  handleSaveEditFxMapping,
  handleCancelEditFxMapping,
  handleStartEditFxMapping,
  handleDeleteFxMapping,
}: BillingCurrencySectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  return (
    <section id="billing-currency" tabIndex={-1} className="scroll-mt-24">
      <Card>
        <CardHeader className="pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Coins className="h-4 w-4" />
                {copy.billingAndCurrency}
              </CardTitle>
              <CardDescription className="text-xs">
                {messages.dashboard.inspectSpendingBreakdown}
              </CardDescription>
            </div>
            <div className="flex items-center gap-2">
              {renderSectionSaveState("billing", billingDirty)}
              <Button
                type="button"
                size="sm"
                onClick={() => void handleSaveCostingSettings("billing")}
                disabled={
                  costingUnavailable ||
                  costingLoading ||
                  costingSaving ||
                  !billingDirty
                }
              >
                {costingSaving ? messages.pricingTemplateDialog.saving : messages.modelsUi.save}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {costingUnavailable ? (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
                {copy.costApiUnavailable}
              </div>
          ) : costingLoading ? (
            <div className="space-y-2">
              <div className="h-9 animate-pulse rounded bg-muted" />
              <div className="h-9 animate-pulse rounded bg-muted" />
              <div className="h-24 animate-pulse rounded bg-muted" />
            </div>
          ) : (
            <>
              <ReportingCurrencyCard
                costingForm={costingForm}
                setCostingForm={setCostingForm}
                normalizedCurrentCosting={normalizedCurrentCosting}
              />

              <div className="rounded-lg border p-4">
                <FxMappingsSummary />
                <FxMappingsTable
                  mappings={normalizedCurrentCosting.endpoint_fx_mappings}
                  modelLabelMap={modelLabelMap}
                  editingMappingKey={editingMappingKey}
                  editingMappingFxRate={editingMappingFxRate}
                  setEditingMappingFxRate={setEditingMappingFxRate}
                  editMappingFxError={editMappingFxError}
                  handleSaveEditFxMapping={handleSaveEditFxMapping}
                  handleCancelEditFxMapping={handleCancelEditFxMapping}
                  handleStartEditFxMapping={handleStartEditFxMapping}
                  handleDeleteFxMapping={handleDeleteFxMapping}
                />
                <FxMappingForm
                  mappingModelId={mappingModelId}
                  setMappingModelId={setMappingModelId}
                  loadMappingConnections={loadMappingConnections}
                  nativeModels={nativeModels}
                  mappingEndpointId={mappingEndpointId}
                  setMappingEndpointId={setMappingEndpointId}
                  mappingEndpointOptions={mappingEndpointOptions}
                  mappingLoading={mappingLoading}
                  mappingFxRate={mappingFxRate}
                  setMappingFxRate={setMappingFxRate}
                  addMappingFxError={addMappingFxError}
                  handleAddFxMapping={handleAddFxMapping}
                />
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
