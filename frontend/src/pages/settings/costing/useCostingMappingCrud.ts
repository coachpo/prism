import { useCallback, useMemo, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Connection, CostingSettingsUpdate, EndpointFxMapping } from "@/lib/types";
import { toast } from "sonner";
import {
  getMappingKey,
  normalizeCostingForm,
  normalizeMappings,
  validateFxRate,
} from "../settingsPageHelpers";

interface UseCostingMappingCrudInput {
  costingForm: CostingSettingsUpdate;
  setCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate>>;
}

function getMessages() {
  return getStaticMessages();
}

export function useCostingMappingCrud({
  costingForm,
  setCostingForm,
}: UseCostingMappingCrudInput) {
  const [mappingConnections, setMappingConnections] = useState<Connection[]>([]);
  const [mappingLoading, setMappingLoading] = useState(false);
  const [mappingModelId, setMappingModelId] = useState("");
  const [mappingEndpointId, setMappingEndpointId] = useState("");
  const [mappingFxRate, setMappingFxRate] = useState("");
  const [editingMappingKey, setEditingMappingKey] = useState<string | null>(null);
  const [editingMappingFxRate, setEditingMappingFxRate] = useState("");

  const normalizedCurrentCosting = useMemo(
    () => normalizeCostingForm(costingForm),
    [costingForm],
  );

  const loadMappingConnections = useCallback(async (modelConfigId: number) => {
    setMappingLoading(true);
    setMappingEndpointId("");

    try {
      const model = await api.models.get(modelConfigId);
      setMappingConnections(model.connections ?? []);
    } catch {
      setMappingConnections([]);
      toast.error(getMessages().settingsCostingData.loadConnectionsFailed);
    } finally {
      setMappingLoading(false);
    }
  }, []);

  const handleAddFxMapping = useCallback(() => {
    if (!mappingModelId || !mappingEndpointId || !mappingFxRate.trim()) {
      toast.error(getMessages().settingsCostingData.mappingFieldsRequired);
      return;
    }

    const fxRateError = validateFxRate(mappingFxRate);
    if (fxRateError) {
      toast.error(fxRateError);
      return;
    }

    const endpointId = Number.parseInt(mappingEndpointId, 10);
    if (Number.isNaN(endpointId)) {
      toast.error(getMessages().settingsCostingData.endpointSelectionInvalid);
      return;
    }

    const duplicate = normalizedCurrentCosting.endpoint_fx_mappings.some(
      (row) => row.model_id === mappingModelId && row.endpoint_id === endpointId,
    );
    if (duplicate) {
      toast.error(getMessages().settingsCostingData.mappingDuplicate);
      return;
    }

    const nextMappings = normalizeMappings([
      ...normalizedCurrentCosting.endpoint_fx_mappings,
      {
        model_id: mappingModelId,
        endpoint_id: endpointId,
        fx_rate: mappingFxRate.trim(),
      },
    ]);

    setCostingForm((prev) => ({ ...prev, endpoint_fx_mappings: nextMappings }));
    setMappingEndpointId("");
    setMappingFxRate("");
  }, [mappingEndpointId, mappingFxRate, mappingModelId, normalizedCurrentCosting, setCostingForm]);

  const handleDeleteFxMapping = useCallback(
    (mapping: EndpointFxMapping) => {
      setCostingForm((prev) => ({
        ...prev,
        endpoint_fx_mappings: prev.endpoint_fx_mappings.filter(
          (row) => !(row.model_id === mapping.model_id && row.endpoint_id === mapping.endpoint_id),
        ),
      }));

      if (editingMappingKey === getMappingKey(mapping)) {
        setEditingMappingKey(null);
        setEditingMappingFxRate("");
      }
    },
    [editingMappingKey, setCostingForm],
  );

  const handleStartEditFxMapping = useCallback((mapping: EndpointFxMapping) => {
    setEditingMappingKey(getMappingKey(mapping));
    setEditingMappingFxRate(mapping.fx_rate);
  }, []);

  const handleCancelEditFxMapping = useCallback(() => {
    setEditingMappingKey(null);
    setEditingMappingFxRate("");
  }, []);

  const handleSaveEditFxMapping = useCallback(() => {
    if (!editingMappingKey) {
      return;
    }

    const fxRateError = validateFxRate(editingMappingFxRate);
    if (fxRateError) {
      toast.error(fxRateError);
      return;
    }

    setCostingForm((prev) => ({
      ...prev,
      endpoint_fx_mappings: normalizeMappings(
        prev.endpoint_fx_mappings.map((row) =>
          getMappingKey(row) === editingMappingKey
            ? { ...row, fx_rate: editingMappingFxRate.trim() }
            : row,
        ),
      ),
    }));
    setEditingMappingKey(null);
    setEditingMappingFxRate("");
  }, [editingMappingFxRate, editingMappingKey, setCostingForm]);

  const addMappingFxError = mappingFxRate ? validateFxRate(mappingFxRate) : null;
  const editMappingFxError = editingMappingKey ? validateFxRate(editingMappingFxRate) : null;

  return {
    addMappingFxError,
    editMappingFxError,
    editingMappingFxRate,
    editingMappingKey,
    handleAddFxMapping,
    handleCancelEditFxMapping,
    handleDeleteFxMapping,
    handleSaveEditFxMapping,
    handleStartEditFxMapping,
    loadMappingConnections,
    mappingConnections,
    mappingEndpointId,
    mappingFxRate,
    mappingLoading,
    mappingModelId,
    setEditingMappingFxRate,
    setMappingEndpointId,
    setMappingFxRate,
    setMappingModelId,
  };
}
