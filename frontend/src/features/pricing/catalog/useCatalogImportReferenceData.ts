import { useCallback, useEffect, useRef, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getSharedConnectionOptions,
  getSharedModels,
} from "@/lib/referenceData";
import type { ConnectionDropdownItem, ModelConfigListItem } from "@/lib/types";

/**
 * Loads the reference data the catalog import dialog needs, but only while the
 * dialog is open. Models and Terminal Targets are read through the same
 * revision-scoped shared caches the rest of the dashboard uses, so an import
 * never shows a list that disagrees with the page behind it.
 */
export function useCatalogImportReferenceData(
  revision: number,
  enabled: boolean,
) {
  const [models, setModels] = useState<ModelConfigListItem[]>([]);
  const [targets, setTargets] = useState<ConnectionDropdownItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const requestGeneration = useRef(0);

  const load = useCallback(async () => {
    const generation = ++requestGeneration.current;
    setLoading(true);
    setError(null);
    try {
      const [nextModels, nextTargets] = await Promise.all([
        getSharedModels(revision, true),
        getSharedConnectionOptions(revision, true),
      ]);
      if (generation !== requestGeneration.current) return;
      setModels(nextModels);
      setTargets(nextTargets);
    } catch (cause) {
      if (generation !== requestGeneration.current) return;
      setError(
        cause instanceof Error
          ? cause.message
          : getStaticMessages().common.requestFailed,
      );
    } finally {
      if (generation === requestGeneration.current) setLoading(false);
    }
  }, [revision]);

  useEffect(() => {
    if (!enabled) return;
    void load();
    return () => {
      requestGeneration.current += 1;
    };
  }, [enabled, load]);

  return { error, loading, models, refresh: load, targets };
}
