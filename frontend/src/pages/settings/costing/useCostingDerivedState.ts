import { useEffect, useMemo, useState } from "react";
import type { CostingSettingsUpdate, ModelConfigListItem } from "@/lib/types";
import { formatTimezoneOffset, formatTimezonePreview, normalizeCostingForm } from "../settingsPageHelpers";

interface UseCostingDerivedStateInput {
  costingForm: CostingSettingsUpdate;
  savedCostingForm: CostingSettingsUpdate | null;
  models: ModelConfigListItem[];
}

export function useCostingDerivedState({
  costingForm,
  savedCostingForm,
  models,
}: UseCostingDerivedStateInput) {
  const [previewInstant, setPreviewInstant] = useState(() => new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setPreviewInstant(new Date()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

  const normalizedCurrentCosting = useMemo(
    () => normalizeCostingForm(costingForm),
    [costingForm],
  );

  const billingDirty = useMemo(() => {
    if (!savedCostingForm) {
      return false;
    }

    return (
      savedCostingForm.report_currency_code !== normalizedCurrentCosting.report_currency_code ||
      savedCostingForm.report_currency_symbol !== normalizedCurrentCosting.report_currency_symbol
    );
  }, [normalizedCurrentCosting, savedCostingForm]);

  const timezoneDirty = useMemo(() => {
    if (!savedCostingForm) {
      return false;
    }

    return (
      (savedCostingForm.timezone_preference ?? null) !==
      (normalizedCurrentCosting.timezone_preference ?? null)
    );
  }, [normalizedCurrentCosting.timezone_preference, savedCostingForm]);

  // One card, one dirty state — the two halves ship in a single request.
  const costingDirty = billingDirty || timezoneDirty;

  const timezonePreviewZone =
    normalizedCurrentCosting.timezone_preference || Intl.DateTimeFormat().resolvedOptions().timeZone;
  const timezonePreviewText = formatTimezonePreview(timezonePreviewZone, previewInstant);
  const timezonePreviewOffset = formatTimezoneOffset(timezonePreviewZone, previewInstant);

  const nativeModels = useMemo(
    () => [...models].sort((a, b) => a.model_id.localeCompare(b.model_id)),
    [models],
  );

  const modelLabelMap = useMemo(
    () => new Map(nativeModels.map((model) => [model.model_id, model.display_name || model.model_id])),
    [nativeModels],
  );

  return {
    billingDirty,
    costingDirty,
    modelLabelMap,
    nativeModels,
    normalizedCurrentCosting,
    timezoneDirty,
    timezonePreviewText,
    timezonePreviewZone,
    timezonePreviewOffset,
  };
}
