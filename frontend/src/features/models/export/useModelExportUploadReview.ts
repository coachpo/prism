import { useCallback, useState } from "react";

import {
  extractClientConfig,
  type ExtractionResult,
} from "./clientConfigExtract";
import type { ExportPlatform, ExportSourceModelRow } from "./exportTypes";

export interface EnhancementDraft {
  fields: Record<string, unknown>;
  overrideFields: string[];
}

interface UseModelExportUploadReviewInput {
  models: ExportSourceModelRow[];
  noExtractedMatchMessage: string;
  platform: ExportPlatform;
  selectedIds: ReadonlySet<number>;
}

export function useModelExportUploadReview({
  models,
  noExtractedMatchMessage,
  platform,
  selectedIds,
}: UseModelExportUploadReviewInput) {
  const [enhancements, setEnhancements] = useState<
    Record<number, EnhancementDraft>
  >({});
  const [confirmedHeaders, setConfirmedHeaders] = useState<
    Record<string, boolean>
  >({});
  const [extraction, setExtraction] = useState<ExtractionResult | null>(null);
  const [extractionError, setExtractionError] = useState<string | null>(null);

  const resetForPlatform = useCallback(() => {
    setEnhancements({});
    setConfirmedHeaders({});
    setExtraction(null);
    setExtractionError(null);
  }, []);

  const handleFileUpload = useCallback(async (file: File | undefined) => {
    if (!file) return;
    // Confirmations belong to one exact parsed file. A replacement upload must
    // never inherit approvals from the previous file.
    setConfirmedHeaders({});
    setExtraction(null);
    setExtractionError(null);
    try {
      const parsed = extractClientConfig(await file.text());
      setExtraction(parsed);
    } catch (error) {
      setExtraction(null);
      setExtractionError(
        error instanceof Error ? error.message : String(error),
      );
    }
  }, []);

  const setHeaderConfirmed = useCallback((id: string, checked: boolean) => {
    setConfirmedHeaders((current) => ({ ...current, [id]: checked }));
  }, []);

  const applyExtraction = useCallback(() => {
    if (!extraction) return;
    const byModelId = new Map(models.map((model) => [model.model_id, model]));
    const nextEnhancements: Record<number, EnhancementDraft> = {
      ...enhancements,
    };
    const matchedIds = new Set<number>();
    for (const candidate of extraction.models) {
      if (candidate.platform !== platform) continue;
      const target = byModelId.get(candidate.modelId);
      if (!target || !selectedIds.has(target.model_config_id)) continue;
      matchedIds.add(target.model_config_id);
      const existing = nextEnhancements[target.model_config_id];
      const fields: Record<string, unknown> = {
        ...(existing?.fields ?? {}),
        ...candidate.fields,
      };
      // Header approvals describe only the current extraction. Reapplying after
      // unchecking a header removes the prior manual value.
      delete fields.headers;
      const confirmedForModel: Record<string, string> = {};
      for (const header of extraction.headerCandidates) {
        if (
          confirmedHeaders[header.id] &&
          header.platform === candidate.platform &&
          header.providerId === candidate.providerId &&
          (header.modelId === undefined || header.modelId === candidate.modelId)
        ) {
          confirmedForModel[header.name] = header.value;
        }
      }
      if (Object.keys(confirmedForModel).length > 0) {
        fields.headers = confirmedForModel;
      }
      const overrideFields = existing?.overrideFields ?? [];
      if (Object.keys(fields).length > 0 || overrideFields.length > 0) {
        nextEnhancements[target.model_config_id] = {
          fields,
          overrideFields,
        };
      } else {
        delete nextEnhancements[target.model_config_id];
      }
    }
    setEnhancements(nextEnhancements);
    setExtractionError(
      matchedIds.size === 0 ? noExtractedMatchMessage : null,
    );
  }, [
    confirmedHeaders,
    enhancements,
    extraction,
    models,
    noExtractedMatchMessage,
    platform,
    selectedIds,
  ]);

  return {
    applyExtraction,
    confirmedHeaders,
    enhancedCount: Object.keys(enhancements).length,
    enhancements,
    extraction,
    extractionError,
    handleFileUpload,
    resetForPlatform,
    setHeaderConfirmed,
  };
}

export type ModelExportUploadReviewState = ReturnType<
  typeof useModelExportUploadReview
>;
