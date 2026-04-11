import type { CostingSettingsUpdate } from "@/lib/types";
import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useCostingDerivedState } from "./costing/useCostingDerivedState";
import { useCostingMappingCrud } from "./costing/useCostingMappingCrud";
import { useCostingSettingsBootstrap } from "./costing/useCostingSettingsBootstrap";
import { useCostingSettingsSave } from "./costing/useCostingSettingsSave";

interface UseCostingSettingsDataInput {
  bumpRevision: () => void;
  primeReportingCurrency: (currency: CostingSettingsUpdate) => void;
  revision: number;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
}

export function useCostingSettingsData({
  bumpRevision,
  primeReportingCurrency,
  revision,
  setRecentlySavedSection,
}: UseCostingSettingsDataInput) {
  const bootstrap = useCostingSettingsBootstrap(revision, primeReportingCurrency);
  const mapping = useCostingMappingCrud({
    costingForm: bootstrap.costingForm,
    setCostingForm: bootstrap.setCostingForm,
  });
  const derived = useCostingDerivedState({
    costingForm: bootstrap.costingForm,
    savedCostingForm: bootstrap.savedCostingForm,
    models: bootstrap.models,
    mappingConnections: mapping.mappingConnections,
  });
  const save = useCostingSettingsSave({
    bumpRevision,
    normalizedCurrentCosting: derived.normalizedCurrentCosting,
    primeReportingCurrency,
    savedCostingForm: bootstrap.savedCostingForm,
    setCostingForm: bootstrap.setCostingForm,
    setCostingUnavailable: bootstrap.setCostingUnavailable,
    setRecentlySavedSection,
    setSavedCostingForm: bootstrap.setSavedCostingForm,
  });

  return {
    ...derived,
    ...mapping,
    ...save,
    costingForm: bootstrap.costingForm,
    costingLoading: bootstrap.costingLoading,
    costingUnavailable: bootstrap.costingUnavailable,
    setCostingForm: bootstrap.setCostingForm,
  };
}
