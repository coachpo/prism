import type { CostingSettingsUpdate } from "@/lib/types";
import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useCallback } from "react";
import { api } from "@/lib/api";
import { useCostingDerivedState } from "./costing/useCostingDerivedState";
import { useCostingSettingsBootstrap } from "./costing/useCostingSettingsBootstrap";
import { useCostingSettingsSave } from "./costing/useCostingSettingsSave";
import { normalizeCostingForm } from "./settingsPageHelpers";

interface UseCostingSettingsDataInput {
  bumpRevision: () => void;
  enabled: boolean;
  primeReportingCurrency: (currency: CostingSettingsUpdate) => void;
  revision: number;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
}

export function useCostingSettingsData({
  bumpRevision,
  enabled,
  primeReportingCurrency,
  revision,
  setRecentlySavedSection,
}: UseCostingSettingsDataInput) {
  const bootstrap = useCostingSettingsBootstrap(enabled, revision, primeReportingCurrency);
  const derived = useCostingDerivedState({
    costingForm: bootstrap.costingForm,
    savedCostingForm: bootstrap.savedCostingForm,
    models: bootstrap.models,
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

  const handleCurrencyMigrationCommitted = useCallback(async () => {
    const fresh = await api.settings.costing.get();
    const normalizedFresh = normalizeCostingForm({
      ...fresh,
      report_currency_code: fresh.report_currency_code ?? "",
      report_currency_symbol: fresh.report_currency_symbol ?? "",
      reporting_currency_epoch: fresh.reporting_currency_epoch ?? undefined,
      expected_updated_at: fresh.updated_at,
    });
    bootstrap.setCostingForm((prev) => ({
      ...prev,
      ...normalizedFresh,
      report_currency_code: normalizedFresh.report_currency_code,
      report_currency_symbol: normalizedFresh.report_currency_symbol,
    }));
    bootstrap.setSavedCostingForm((prev) => ({
      ...normalizedFresh,
      report_currency_code: normalizedFresh.report_currency_code,
      report_currency_symbol: normalizedFresh.report_currency_symbol,
      timezone_preference: normalizedFresh.timezone_preference ?? prev?.timezone_preference ?? null,
    }));
    primeReportingCurrency(normalizedFresh);
    bumpRevision();
  }, [bootstrap, bumpRevision, primeReportingCurrency]);

  return {
    ...derived,
    ...save,
    costingForm: bootstrap.costingForm,
    costingLoading: bootstrap.costingLoading,
    costingUnavailable: bootstrap.costingUnavailable,
    setCostingForm: bootstrap.setCostingForm,
    handleCurrencyMigrationCommitted,
  };
}
