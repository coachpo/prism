import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** OpenCode's independent read snapshot and operator selection. */
export function useOpenCodeExportSource() {
  const [searchText, setSearchText] = useState("");
  const [familyFilter, setFamilyFilter] = useState("all");
  const [metadataFilter, setMetadataFilter] = useState<
    "all" | "complete" | "incomplete"
  >("all");
  const [priceCompleteOnly, setPriceCompleteOnly] = useState(false);
  const sourceQuery = useQuery({
    queryKey: ["opencode-export-source"],
    queryFn: ({ signal }) => api.opencodeExport.fetchOpenCodeExportSource(signal),
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
    retry: false,
  });
  const models = useMemo(() => sourceQuery.data?.models ?? [], [sourceQuery.data]);
  const selectableIds = useMemo(
    () =>
      new Set(
        models
          .filter(
            (model) =>
              model.direct_request_enabled === true &&
              model.is_enabled &&
              model.selectable,
          )
          .map((model) => model.model_config_id),
      ),
    [models],
  );
  const [selectedIds, setSelectedIds] = useState(new Set<number>());
  const [previousDigest, setPreviousDigest] = useState<string | null>(null);
  const digest = sourceQuery.data?.source_digest ?? null;
  if (digest && previousDigest !== digest) {
    setPreviousDigest(digest);
    setSelectedIds((current) =>
      previousDigest === null
        ? new Set(selectableIds)
        : new Set([...current].filter((id) => selectableIds.has(id))),
    );
  }
  const visibleModels = useMemo(
    () =>
      models.filter((model) => {
        if (!model.direct_request_enabled) return false;
        const searchable = [
          model.model_id,
          model.display_name,
          model.merged_metadata.name,
        ]
          .filter(Boolean)
          .join(" ")
          .toLowerCase();
        if (
          searchText.trim() &&
          !searchable.includes(searchText.trim().toLowerCase())
        ) {
          return false;
        }
        if (familyFilter !== "all" && familyFilter !== model.api_family) {
          return false;
        }
        const complete = model.missing_metadata.length === 0;
        if (metadataFilter === "complete" && !complete) return false;
        if (metadataFilter === "incomplete" && complete) return false;
        return !priceCompleteOnly || model.price_risk.exportable;
      }),
    [models, searchText, familyFilter, metadataFilter, priceCompleteOnly],
  );
  const toggleModel = useCallback(
    (id: number, checked: boolean) => {
      setSelectedIds((current) => {
        const next = new Set(current);
        if (checked && selectableIds.has(id)) next.add(id);
        else next.delete(id);
        return next;
      });
    },
    [selectableIds],
  );
  const batchSelectVisible = () =>
    setSelectedIds(
      (current) =>
        new Set([
          ...current,
          ...visibleModels
            .filter((model) => selectableIds.has(model.model_config_id))
            .map((model) => model.model_config_id),
        ]),
    );
  const batchClearVisible = () =>
    setSelectedIds(
      (current) =>
        new Set(
          [...current].filter(
            (id) => !visibleModels.some((model) => model.model_config_id === id),
          ),
        ),
    );
  const selected = models.filter((model) => selectedIds.has(model.model_config_id));
  const selectedRiskSummary = {
    metadataIncomplete: selected.filter(
      (model) => model.missing_metadata.length > 0,
    ).length,
    costOmitted: selected.filter((model) => !model.price_risk.exportable).length,
  };

  return {
    sourceQuery,
    visibleModels,
    selectedIds,
    selectedRiskSummary,
    toggleModel,
    batchSelectVisible,
    batchClearVisible,
    searchText,
    setSearchText,
    familyFilter,
    setFamilyFilter,
    metadataFilter,
    setMetadataFilter,
    priceCompleteOnly,
    setPriceCompleteOnly,
    sourceActionsBlocked:
      sourceQuery.isError || sourceQuery.isFetching || !sourceQuery.data,
  };
}

export type OpenCodeExportSourceState = ReturnType<typeof useOpenCodeExportSource>;
