import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { isValidCurrencyCode } from "@/lib/costing";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearUserTimezonePreference } from "@/lib/timezone";
import type { CostingSettingsUpdate } from "@/lib/types";
import { toast } from "sonner";
import type { SettingsSaveSection } from "../settingsSaveTypes";
import { normalizeCostingForm, validateMappings } from "../settingsPageHelpers";

type CostingSaveSection = "billing" | "timezone";

function getMessages() {
  return getStaticMessages();
}

interface UseCostingSettingsSaveInput {
  normalizedCurrentCosting: CostingSettingsUpdate;
  savedCostingForm: CostingSettingsUpdate | null;
  setCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate>>;
  setCostingUnavailable: Dispatch<SetStateAction<boolean>>;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
  setSavedCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate | null>>;
}

export function useCostingSettingsSave({
  normalizedCurrentCosting,
  savedCostingForm,
  setCostingForm,
  setCostingUnavailable,
  setRecentlySavedSection,
  setSavedCostingForm,
}: UseCostingSettingsSaveInput) {
  const [costingSaving, setCostingSaving] = useState(false);

  const handleSaveCostingSettings = useCallback(
    async (section: CostingSaveSection) => {
      const baseline = savedCostingForm ?? normalizedCurrentCosting;
      const validationError =
        section === "billing"
          ? validateBillingSection(normalizedCurrentCosting)
          : validateTimezoneSection(baseline);

      if (validationError) {
        toast.error(validationError);
        return;
      }

      const payload = buildSavePayload(section, normalizedCurrentCosting, baseline);

      setCostingSaving(true);
      try {
        if (section === "billing") {
          const saved = await api.settings.costing.update(payload);
          const normalizedSaved = normalizeCostingForm(saved);
          setCostingForm((prev) => ({
            ...prev,
            report_currency_code: normalizedSaved.report_currency_code,
            report_currency_symbol: normalizedSaved.report_currency_symbol,
            endpoint_fx_mappings: normalizedSaved.endpoint_fx_mappings,
          }));
          setSavedCostingForm((prev) => ({
            report_currency_code: normalizedSaved.report_currency_code,
            report_currency_symbol: normalizedSaved.report_currency_symbol,
            endpoint_fx_mappings: normalizedSaved.endpoint_fx_mappings,
            timezone_preference: prev?.timezone_preference ?? baseline.timezone_preference,
          }));
          setRecentlySavedSection("billing");
          toast.success(getMessages().settingsCostingData.billingSaved);
        } else {
          const saved = await api.settings.timezone.update({
            timezone_preference: normalizedCurrentCosting.timezone_preference ?? null,
          });
          clearUserTimezonePreference();
          setCostingForm((prev) => ({
            ...prev,
            timezone_preference: saved.timezone_preference ?? null,
          }));
          setSavedCostingForm((prev) => ({
            report_currency_code: prev?.report_currency_code ?? baseline.report_currency_code,
            report_currency_symbol:
              prev?.report_currency_symbol ?? baseline.report_currency_symbol,
            endpoint_fx_mappings:
              prev?.endpoint_fx_mappings ?? baseline.endpoint_fx_mappings,
            timezone_preference: saved.timezone_preference ?? null,
          }));
          setRecentlySavedSection("timezone");
          toast.success(getMessages().settingsCostingData.timezoneSaved);
        }

        setCostingUnavailable(false);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getMessages().settingsCostingData.saveFailed);
      } finally {
        setCostingSaving(false);
      }
    },
    [
      normalizedCurrentCosting,
      savedCostingForm,
      setCostingForm,
      setCostingUnavailable,
      setRecentlySavedSection,
      setSavedCostingForm,
    ],
  );

  return {
    costingSaving,
    handleSaveCostingSettings,
  };
}

function validateBillingSection(costing: CostingSettingsUpdate): string | null {
  const messages = getMessages().settingsCostingData;
  if (!isValidCurrencyCode(costing.report_currency_code)) {
    return messages.reportCurrencyRequired;
  }

  if (!costing.report_currency_symbol) {
    return messages.reportCurrencyRequired;
  }

  if (costing.report_currency_symbol.length > 5) {
    return messages.reportCurrencySymbolLength;
  }

  return validateMappings(costing.endpoint_fx_mappings);
}

function validateTimezoneSection(baseline: CostingSettingsUpdate): string | null {
  const messages = getMessages().settingsCostingData;
  if (!isValidCurrencyCode(baseline.report_currency_code) || !baseline.report_currency_symbol) {
    return messages.saveBillingBeforeTimezone;
  }

  const baselineMappingError = validateMappings(baseline.endpoint_fx_mappings);
  if (baselineMappingError) {
    return messages.fixMappingErrorsBeforeTimezone;
  }

  return null;
}

function buildSavePayload(
  section: CostingSaveSection,
  normalizedCurrentCosting: CostingSettingsUpdate,
  baseline: CostingSettingsUpdate,
): CostingSettingsUpdate {
  if (section === "billing") {
    return {
      report_currency_code: normalizedCurrentCosting.report_currency_code,
      report_currency_symbol: normalizedCurrentCosting.report_currency_symbol,
      endpoint_fx_mappings: normalizedCurrentCosting.endpoint_fx_mappings,
      timezone_preference: baseline.timezone_preference,
    };
  }

  return {
    report_currency_code: baseline.report_currency_code,
    report_currency_symbol: baseline.report_currency_symbol,
    endpoint_fx_mappings: baseline.endpoint_fx_mappings,
    timezone_preference: normalizedCurrentCosting.timezone_preference,
  };
}
