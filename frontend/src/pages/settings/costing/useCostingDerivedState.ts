import { useMemo } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Connection, CostingSettingsUpdate, ModelConfigListItem } from "@/lib/types";
import {
  areMappingsEqual,
  formatTimezonePreview,
  getConnectionName,
  normalizeCostingForm,
} from "../settingsPageHelpers";

interface UseCostingDerivedStateInput {
  costingForm: CostingSettingsUpdate;
  savedCostingForm: CostingSettingsUpdate | null;
  models: ModelConfigListItem[];
  mappingConnections: Connection[];
}

export function useCostingDerivedState({
  costingForm,
  savedCostingForm,
  models,
  mappingConnections,
}: UseCostingDerivedStateInput) {
  const messages = getStaticMessages();
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
      savedCostingForm.report_currency_symbol !== normalizedCurrentCosting.report_currency_symbol ||
      !areMappingsEqual(
        savedCostingForm.endpoint_fx_mappings,
        normalizedCurrentCosting.endpoint_fx_mappings,
      )
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

  const timezonePreviewZone =
    normalizedCurrentCosting.timezone_preference || Intl.DateTimeFormat().resolvedOptions().timeZone;
  const timezonePreviewText = formatTimezonePreview(timezonePreviewZone);

  const nativeModels = useMemo(
    () =>
      models
        .filter((model) => model.model_type === "native")
        .sort((a, b) => a.model_id.localeCompare(b.model_id)),
    [models],
  );

  const modelLabelMap = useMemo(
    () => new Map(nativeModels.map((model) => [model.model_id, model.display_name || model.model_id])),
    [nativeModels],
  );

  const mappingEndpointOptions = useMemo(
    () =>
      Array.from(
        new Map(
          mappingConnections.map((connection) => [
            connection.endpoint_id,
            {
              endpointId: connection.endpoint_id,
              label:
                connection.endpoint?.name ||
                connection.endpoint?.base_url ||
                getConnectionName(connection) ||
                messages.common.endpointWithId(String(connection.endpoint_id)),
            },
          ]),
        ).values(),
      ).sort((a, b) => a.endpointId - b.endpointId),
    [mappingConnections, messages.common],
  );

  return {
    billingDirty,
    modelLabelMap,
    mappingEndpointOptions,
    nativeModels,
    normalizedCurrentCosting,
    timezoneDirty,
    timezonePreviewText,
    timezonePreviewZone,
  };
}
