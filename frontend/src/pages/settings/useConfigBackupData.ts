import { type ChangeEvent, useMemo, useRef, useState } from "react";
import { z } from "zod";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { ConfigImportSchema } from "@/lib/configImportValidation";
import type { ConfigImportPreviewResponse, ConfigImportRequest } from "@/lib/types";
import { toast } from "sonner";

interface UseConfigBackupDataInput {
  bumpRevision: () => void;
}

export function useConfigBackupData({ bumpRevision }: UseConfigBackupDataInput) {
  const [importing, setImporting] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportSecretsAcknowledged, setExportSecretsAcknowledged] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [parsedConfig, setParsedConfig] = useState<ConfigImportRequest | null>(null);
  const [previewResult, setPreviewResult] = useState<ConfigImportPreviewResponse | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const currentSelectionTokenRef = useRef(0);

  const resetSelectedFile = () => {
    setSelectedFile(null);
    setParsedConfig(null);
    setPreviewResult(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const importSummary = useMemo(() => {
    const endpointsCount = previewResult?.endpoints_imported ?? parsedConfig?.endpoints?.length ?? 0;
    const strategiesCount =
      previewResult?.strategies_imported ?? parsedConfig?.loadbalance_strategies?.length ?? 0;
    const modelsCount = previewResult?.models_imported ?? parsedConfig?.models?.length ?? 0;
    const connectionsCount =
      previewResult?.connections_imported ??
      parsedConfig?.models?.reduce((total, model) => total + (model.connections?.length ?? 0), 0) ??
      0;

    return {
      endpointsCount,
      strategiesCount,
      modelsCount,
      connectionsCount,
    };
  }, [parsedConfig, previewResult]);

  const handleExport = async () => {
    const messages = getStaticMessages();

    setExporting(true);
    try {
      const data = await api.config.export();
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      const date = new Date().toISOString().split("T")[0];
      anchor.href = url;
      anchor.download = `gateway-config-${date}.json`;
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(messages.settingsBackupData.exportSucceeded);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.exportFailed);
    } finally {
      setExporting(false);
    }
  };

  const handleFileSelect = async (event: ChangeEvent<HTMLInputElement>) => {
    const messages = getStaticMessages();
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }

    const selectionToken = currentSelectionTokenRef.current + 1;
    currentSelectionTokenRef.current = selectionToken;
    setSelectedFile(file);
    setParsedConfig(null);
    setPreviewResult(null);

    try {
      const text = await file.text();
      if (currentSelectionTokenRef.current != selectionToken) {
        return;
      }
      const parsed = JSON.parse(text);
      const validation = ConfigImportSchema.safeParse(parsed);

      if (!validation.success) {
        const errors = validation.error.issues
          .map((issue: z.ZodIssue) => `${issue.path.join(".")}: ${issue.message}`)
          .join(", ");
        throw new Error(messages.settingsBackupData.invalidConfigPayload(errors));
      }

      const config = validation.data as ConfigImportRequest;
      const preview = await api.config.previewImport(config);
      if (currentSelectionTokenRef.current != selectionToken) {
        return;
      }
      setParsedConfig(config);
      setPreviewResult(preview);
      if (!preview.ready && preview.blocking_errors.length > 0) {
        toast.error(preview.blocking_errors[0]);
      }
    } catch (error) {
      if (currentSelectionTokenRef.current != selectionToken) {
        return;
      }
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.invalidJsonFile);
      resetSelectedFile();
    }
  };

  const handleImport = async () => {
    const messages = getStaticMessages();
    if (!parsedConfig || !previewResult?.ready) {
      if (previewResult && previewResult.blocking_errors.length > 0) {
        toast.error(previewResult.blocking_errors[0]);
      }
      return;
    }

    setImporting(true);
    try {
      const result = await api.config.import(parsedConfig);
      toast.success(messages.settingsBackupData.importSucceeded(
        String(result.endpoints_imported),
        String(result.strategies_imported),
        String(result.models_imported),
        String(result.connections_imported),
      ));
      resetSelectedFile();
      bumpRevision();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.importFailed);
    } finally {
      setImporting(false);
    }
  };

  return {
    exportSecretsAcknowledged,
    exporting,
    fileInputRef,
    handleExport,
    handleFileSelect,
    handleImport,
    importSummary,
    importing,
    parsedConfig,
    previewResult,
    selectedFile,
    setExportSecretsAcknowledged,
  };
}
