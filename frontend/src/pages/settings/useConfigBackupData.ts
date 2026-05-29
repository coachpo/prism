import { type ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { ConfigImportSchema } from "@/lib/configImportValidation";
import type { ConfigImportPreviewResponse, ConfigImportRequest } from "@/lib/types";
import { toast } from "sonner";

type ConfigExportMode = "safe" | "dangerous";
type PreviewInvalidationReason = "bundle_changed" | "profile_changed";

interface UseConfigBackupDataInput {
  bumpRevision: () => void;
  selectedProfileId: number | null;
}

function buildProfileConfigExportFilename(mode: ConfigExportMode, now: Date = new Date()) {
  const date = now.toISOString().split("T")[0];
  return mode === "dangerous"
    ? `prism-profile-config-with-secrets-v3-${date}.json`
    : `prism-profile-config-v3-${date}.json`;
}

export function useConfigBackupData({ bumpRevision, selectedProfileId }: UseConfigBackupDataInput) {
  const [importing, setImporting] = useState(false);
  const [exportingMode, setExportingMode] = useState<ConfigExportMode | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [exportSecretsAcknowledged, setExportSecretsAcknowledged] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [parsedConfig, setParsedConfig] = useState<ConfigImportRequest | null>(null);
  const [previewResult, setPreviewResult] = useState<ConfigImportPreviewResponse | null>(null);
  const [previewInvalidationReason, setPreviewInvalidationReason] =
    useState<PreviewInvalidationReason | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const currentSelectionTokenRef = useRef(0);
  const currentPreviewRequestTokenRef = useRef(0);
  const currentPreviewBindingRef = useRef<{
    selectionToken: number;
    profileId: number | null;
  } | null>(null);
  const latestSelectedProfileIdRef = useRef(selectedProfileId);

  const clearPreviewState = useCallback((reason: PreviewInvalidationReason | null) => {
    currentPreviewBindingRef.current = null;
    currentPreviewRequestTokenRef.current += 1;
    setPreviewResult(null);
    setPreviewInvalidationReason(reason);
    setPreviewing(false);
  }, []);

  const resetSelectedFile = useCallback(() => {
    currentSelectionTokenRef.current += 1;
    setSelectedFile(null);
    setParsedConfig(null);
    clearPreviewState(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, [clearPreviewState]);

  useEffect(() => {
    const previousProfileId = latestSelectedProfileIdRef.current;
    latestSelectedProfileIdRef.current = selectedProfileId;

    if (previousProfileId === selectedProfileId) {
      return;
    }

    if (selectedFile || parsedConfig || previewResult) {
      clearPreviewState("profile_changed");
    }
  }, [clearPreviewState, parsedConfig, previewResult, selectedFile, selectedProfileId]);

  const importSummary = useMemo(() => {
    const endpointsCount = previewResult?.endpoints_imported ?? parsedConfig?.endpoints?.length ?? 0;
    const strategiesCount =
      previewResult?.strategies_imported ?? parsedConfig?.loadbalance_strategies?.length ?? 0;
    const modelsCount = previewResult?.models_imported ?? parsedConfig?.models?.length ?? 0;
    const connectionsCount = previewResult?.connections_imported ?? parsedConfig?.connections?.length ?? 0;

    return {
      endpointsCount,
      strategiesCount,
      modelsCount,
      connectionsCount,
    };
  }, [parsedConfig, previewResult]);

  const downloadExport = async (mode: ConfigExportMode) => {
    const messages = getStaticMessages();

    if (mode === "dangerous" && !exportSecretsAcknowledged) {
      toast.error(messages.settingsBackupData.acknowledgeSecretsBeforeExport);
      return;
    }

    setExportingMode(mode);
    try {
      const data = mode === "dangerous"
        ? await api.config.exportWithSecrets()
        : await api.config.export();
      const blob = new Blob([JSON.stringify(data, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = buildProfileConfigExportFilename(mode);
      anchor.click();
      URL.revokeObjectURL(url);
      toast.success(messages.settingsBackupData.exportSucceeded);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.exportFailed);
    } finally {
      setExportingMode(null);
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
    clearPreviewState("bundle_changed");

    try {
      const text = await file.text();
      if (currentSelectionTokenRef.current !== selectionToken) {
        return;
      }
      const parsed = JSON.parse(text);
      const validation = ConfigImportSchema.safeParse(parsed);

      if (!validation.success) {
        const errors = validation.error.issues
          .map((issue) => `${issue.path.join(".")}: ${issue.message}`)
          .join(", ");
        throw new Error(messages.settingsBackupData.invalidConfigPayload(errors));
      }

      setParsedConfig(validation.data as ConfigImportRequest);
    } catch (error) {
      if (currentSelectionTokenRef.current !== selectionToken) {
        return;
      }
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.invalidJsonFile);
      resetSelectedFile();
    }
  };

  const handlePreviewImport = async () => {
    const messages = getStaticMessages();
    if (!parsedConfig) {
      return;
    }

    const selectionToken = currentSelectionTokenRef.current;
    const profileId = latestSelectedProfileIdRef.current;
    const previewRequestToken = currentPreviewRequestTokenRef.current + 1;
    currentPreviewRequestTokenRef.current = previewRequestToken;
    currentPreviewBindingRef.current = null;
    setPreviewResult(null);
    setPreviewing(true);

    try {
      const preview = await api.config.previewImport(parsedConfig);
      if (currentPreviewRequestTokenRef.current !== previewRequestToken) {
        return;
      }
      if (currentSelectionTokenRef.current !== selectionToken) {
        return;
      }
      if (latestSelectedProfileIdRef.current !== profileId) {
        return;
      }

      currentPreviewBindingRef.current = {
        selectionToken,
        profileId,
      };
      setPreviewInvalidationReason(null);
      setPreviewResult(preview);
      if (!preview.ready && preview.blocking_errors.length > 0) {
        toast.error(preview.blocking_errors[0]);
      }
    } catch (error) {
      if (currentPreviewRequestTokenRef.current !== previewRequestToken) {
        return;
      }
      toast.error(error instanceof Error ? error.message : messages.settingsBackupData.previewFailed);
      clearPreviewState("bundle_changed");
    } finally {
      if (currentPreviewRequestTokenRef.current === previewRequestToken) {
        setPreviewing(false);
      }
    }
  };

  const handleImport = async () => {
    const messages = getStaticMessages();
    const previewBinding = currentPreviewBindingRef.current;

    if (
      !parsedConfig ||
      !previewResult?.ready ||
      !previewBinding ||
      previewBinding.selectionToken !== currentSelectionTokenRef.current ||
      previewBinding.profileId !== latestSelectedProfileIdRef.current
    ) {
      toast.error(previewResult?.blocking_errors[0] ?? messages.settingsBackupData.previewRequiredBeforeImport);
      return;
    }

    setImporting(true);
    try {
      const result = await api.config.import(parsedConfig, previewResult.preview_token);
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
    exportingMode,
    fileInputRef,
    handleDangerousExport: () => downloadExport("dangerous"),
    handleFileSelect,
    handleImport,
    handlePreviewImport,
    handleSafeExport: () => downloadExport("safe"),
    importSummary,
    importing,
    parsedConfig,
    previewInvalidationReason,
    previewResult,
    previewing,
    selectedFile,
    setExportSecretsAcknowledged,
  };
}
