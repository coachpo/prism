import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { isValidCurrencyCode } from "@/lib/costing";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearUserTimezonePreference } from "@/lib/timezone";
import type { CostingSettingsUpdate } from "@/lib/types";
import { toast } from "sonner";
import type { SettingsSaveSection } from "../settingsSaveTypes";
import { normalizeCostingForm } from "./costingForm";

function getMessages() {
  return getStaticMessages();
}

interface UseCostingSettingsSaveInput {
  bumpRevision: () => void;
  normalizedCurrentCosting: CostingSettingsUpdate;
  primeReportingCurrency: (currency: CostingSettingsUpdate) => void;
  savedCostingForm: CostingSettingsUpdate | null;
  setCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate>>;
  setCostingUnavailable: Dispatch<SetStateAction<boolean>>;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
  setSavedCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate | null>>;
}

export function useCostingSettingsSave({
  bumpRevision,
  normalizedCurrentCosting,
  primeReportingCurrency,
  savedCostingForm,
  setCostingForm,
  setCostingUnavailable,
  setRecentlySavedSection,
  setSavedCostingForm,
}: UseCostingSettingsSaveInput) {
  const [costingSaving, setCostingSaving] = useState(false);

  /**
   * Currency and timezone were never two saves: both were already one
   * `PUT /api/settings/costing` whose payload had to carry the other side's
   * baseline. They now travel together, so neither can silently overwrite the
   * other with a stale value.
   */
  const handleSaveCostingSettings = useCallback(
    async () => {
      const baseline = savedCostingForm ?? normalizedCurrentCosting;
      const validationError = validateCosting(normalizedCurrentCosting);

      if (validationError) {
        toast.error(validationError);
        return;
      }

      setCostingSaving(true);
      try {
        const saved = await api.settings.costing.update({
          report_currency_symbol: normalizedCurrentCosting.report_currency_symbol,
          timezone_preference: normalizedCurrentCosting.timezone_preference ?? null,
          expected_updated_at: baseline.expected_updated_at ?? null,
        });
        const normalizedSaved = normalizeCostingForm(saved);
        clearUserTimezonePreference();
        setCostingForm((prev) => ({
          ...prev,
          report_currency_code: normalizedSaved.report_currency_code,
          report_currency_symbol: normalizedSaved.report_currency_symbol,
          timezone_preference: normalizedSaved.timezone_preference,
          expected_updated_at: saved.updated_at ?? null,
        }));
        setSavedCostingForm({
          report_currency_code: normalizedSaved.report_currency_code,
          report_currency_symbol: normalizedSaved.report_currency_symbol,
          timezone_preference: normalizedSaved.timezone_preference,
          expected_updated_at: saved.updated_at ?? null,
        });
        primeReportingCurrency(normalizedSaved);
        bumpRevision();
        setRecentlySavedSection("costing");
        toast.success(getMessages().settingsCostingData.billingSaved);

        setCostingUnavailable(false);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getMessages().settingsCostingData.saveFailed);
      } finally {
        setCostingSaving(false);
      }
    },
    [
      bumpRevision,
      normalizedCurrentCosting,
      primeReportingCurrency,
      savedCostingForm,
      setCostingForm,
      setCostingUnavailable,
      setRecentlySavedSection,
      setSavedCostingForm,
    ],
  );

  return {
    costingSaving,
    costingValidationError: validateCosting(normalizedCurrentCosting),
    handleSaveCostingSettings,
  };
}

/** Returns the localized reason the card cannot be saved, or `null`. */
export function validateCosting(costing: CostingSettingsUpdate): string | null {
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

  return null;
}
