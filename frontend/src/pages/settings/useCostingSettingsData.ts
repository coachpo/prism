import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useCostingDerivedState } from "./costing/useCostingDerivedState";
import { useCostingMappingCrud } from "./costing/useCostingMappingCrud";
import { useCostingSettingsBootstrap } from "./costing/useCostingSettingsBootstrap";
import { useCostingSettingsSave } from "./costing/useCostingSettingsSave";

interface UseCostingSettingsDataInput {
  revision: number;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
}

export function useCostingSettingsData({
  revision,
  setRecentlySavedSection,
}: UseCostingSettingsDataInput) {
  const bootstrap = useCostingSettingsBootstrap(revision);
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
    normalizedCurrentCosting: derived.normalizedCurrentCosting,
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
