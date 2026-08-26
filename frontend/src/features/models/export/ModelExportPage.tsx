import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  OperatorCallout,
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorSectionCard,
  OperatorTableShell,
} from "@/shared/design-system";
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import {
  fetchModelExportSource,
  renderModelExport,
} from "@/lib/api/modelExport";
import type {
  ExportPlatform,
  ExportRenderResponse,
  ExportSourceModelRow,
  ExportSourceResponse,
  ManualEnhancementWire,
} from "./exportTypes";
import {
  extractClientConfig,
  type ExtractionResult,
} from "./clientConfigExtract";
import { PlatformKeyDialog, type KeyDecision } from "./PlatformKeyDialog";
import { ExportResultSheet } from "./ExportResultSheet";

interface EnhancementDraft {
  fields: Record<string, unknown>;
  overrideFields: string[];
}

interface ExportSelectionState {
  platform: ExportPlatform;
  sourceDigest: string | null;
  ids: Set<number>;
  defaultModelConfigId?: number;
}

function useExportSelection(
  platform: ExportPlatform,
  source: ExportSourceResponse | undefined,
  selectableIds: Set<number>,
) {
  const [state, setState] = useState<ExportSelectionState>({
    platform,
    sourceDigest: null,
    ids: new Set(),
  });

  if (
    source?.platform === platform &&
    (state.platform !== platform || state.sourceDigest !== source.source_digest)
  ) {
    const adoptDefaults =
      state.platform !== platform || state.sourceDigest === null;
    const ids = adoptDefaults
      ? new Set(
          source.models
            .filter((model) => model.default_selected)
            .map((model) => model.model_config_id),
        )
      : new Set([...state.ids].filter((id) => selectableIds.has(id)));
    const next: ExportSelectionState = {
      platform,
      sourceDigest: source.source_digest,
      ids,
      defaultModelConfigId:
        state.defaultModelConfigId !== undefined &&
        ids.has(state.defaultModelConfigId)
          ? state.defaultModelConfigId
          : undefined,
    };
    setState(next);
    return [next, setState] as const;
  }

  return [state, setState] as const;
}

const DEFAULT_PROVIDER_ID = "prism";

function defaultGatewayOrigin(): string {
  return getEffectiveBackendOrigin().origin;
}

function normalizeGatewayOrigin(value: string): string | null {
  try {
    const parsed = new URL(value.trim());
    if (
      (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
      parsed.username !== "" ||
      parsed.password !== "" ||
      parsed.search !== "" ||
      parsed.hash !== "" ||
      (parsed.pathname !== "" && parsed.pathname !== "/")
    ) {
      return null;
    }
    return parsed.origin;
  } catch {
    return null;
  }
}

const WARNING_LABEL_KEYS: Record<string, string> = {
  price_no_template: "warnNoTemplate",
  price_currency_not_usd: "warnNotUsd",
  price_unit_not_per_1m: "warnNotPerMillion",
  price_incomplete_components: "warnIncomplete",
  pricing_component_missing: "warnIncomplete",
  price_reasoning_mismatch: "warnReasoningMismatch",
  price_target_conflict: "warnTargetConflict",
  price_peak_valley_unrepresentable: "warnPeakValley",
  price_tier_unrepresentable: "warnTierUnrepresentable",
  enrichment_unavailable: "warnEnrichmentUnavailable",
  metadata_incomplete: "warnMetadataIncomplete",
  pi_compat_may_require_manual_override: "warnPiCompatManual",
  unsupported_input_modality: "warnUnsupportedInputModality",
  variants_unrepresentable: "warnVariants",
  thinking_level_map_unrepresentable: "warnThinkingMap",
  mixed_base_urls: "warnMixedBaseUrls",
  mixed_credentials: "warnMixedCredentials",
};

export function ModelExportPage() {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;

  const [platform, setPlatform] = useState<ExportPlatform>("pi");
  const [searchText, setSearchText] = useState("");
  const [familyFilter, setFamilyFilter] = useState<string>("all");
  const [metadataFilter, setMetadataFilter] = useState<
    "all" | "complete" | "incomplete"
  >("all");
  // only-price-complete is a default-off frontend filter/batch-select aid; it
  // never mutates the operator's existing selection.
  const [priceCompleteOnly, setPriceCompleteOnly] = useState(false);
  const [gatewayOrigin, setGatewayOrigin] = useState(defaultGatewayOrigin);
  const [providerId, setProviderId] = useState(DEFAULT_PROVIDER_ID);
  const [enhancements, setEnhancements] = useState<
    Record<number, EnhancementDraft>
  >({});
  const [confirmedHeaders, setConfirmedHeaders] = useState<
    Record<string, boolean>
  >({});
  const [extraction, setExtraction] = useState<ExtractionResult | null>(null);
  const [extractionError, setExtractionError] = useState<string | null>(null);
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [renderResult, setRenderResult] = useState<ExportRenderResponse | null>(
    null,
  );
  const [renderError, setRenderError] = useState<string | null>(null);
  const [renderStale, setRenderStale] = useState(false);

  const sourceQuery = useQuery<ExportSourceResponse>({
    queryKey: ["model-export-source", platform],
    queryFn: ({ signal }) => fetchModelExportSource(platform, signal),
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
  });

  const models = useMemo(
    () => sourceQuery.data?.models ?? [],
    [sourceQuery.data],
  );
  const selectableIds = useMemo(
    () =>
      new Set(models.filter((m) => m.selectable).map((m) => m.model_config_id)),
    [models],
  );
  // Selection reconciliation happens during render, following React's
  // prop-derived state pattern: first load adopts backend defaults, while a
  // new digest on the same platform intersects the existing selection.
  const [selection, setSelection] = useExportSelection(
    platform,
    sourceQuery.data,
    selectableIds,
  );
  const selectedIds = selection.ids;
  const defaultModelConfigId = selection.defaultModelConfigId;

  const updateSelectedIds = useCallback(
    (update: (current: Set<number>) => Set<number>) => {
      setSelection((current) => {
        const ids = update(current.ids);
        return {
          ...current,
          ids,
          defaultModelConfigId:
            current.defaultModelConfigId !== undefined &&
            ids.has(current.defaultModelConfigId)
              ? current.defaultModelConfigId
              : undefined,
        };
      });
    },
    [setSelection],
  );

  const handlePlatformSwitch = useCallback(
    (next: ExportPlatform) => {
      if (next === platform) return;
      setSelection({
        platform: next,
        sourceDigest: null,
        ids: new Set(),
      });
      setRenderResult(null);
      setRenderError(null);
      setRenderStale(false);
      setEnhancements({});
      setConfirmedHeaders({});
      setExtraction(null);
      setExtractionError(null);
      setKeyDialogOpen(false);
      setPlatform(next);
    },
    [platform, setSelection],
  );

  const visibleModels = useMemo(() => {
    const needle = searchText.trim().toLowerCase();
    return models.filter((model) => {
      const mergedName = model.merged_metadata.name;
      const searchable = [
        model.model_id,
        model.display_name,
        typeof mergedName === "string" ? mergedName : null,
      ]
        .filter((value): value is string => typeof value === "string")
        .join(" ")
        .toLowerCase();
      if (needle && !searchable.includes(needle)) return false;
      if (familyFilter !== "all" && model.api_family !== familyFilter)
        return false;
      const fieldStates = Object.values(
        model.platform_completeness.metadata_fields,
      );
      const metadataComplete =
        fieldStates.length > 0
          ? fieldStates.every(Boolean)
          : model.missing_metadata.length === 0;
      if (metadataFilter === "complete" && !metadataComplete) return false;
      if (metadataFilter === "incomplete" && metadataComplete) return false;
      if (priceCompleteOnly && !model.price_risk.exportable)
        return false;
      return true;
    });
  }, [
    models,
    searchText,
    familyFilter,
    metadataFilter,
    priceCompleteOnly,
  ]);

  const selectedCount = selectedIds.size;
  const selectedModels = useMemo(
    () => models.filter((model) => selectedIds.has(model.model_config_id)),
    [models, selectedIds],
  );
  const selectedRiskSummary = useMemo(() => {
    let metadataIncomplete = 0;
    let costOmitted = 0;
    let enrichmentUnavailable = 0;
    for (const model of selectedModels) {
      const fieldStates = Object.values(
        model.platform_completeness.metadata_fields,
      );
      const metadataComplete =
        fieldStates.length > 0
          ? fieldStates.every(Boolean)
          : model.missing_metadata.length === 0;
      if (!metadataComplete) metadataIncomplete += 1;
      if (!model.price_risk.exportable) costOmitted += 1;
      if ((model.warnings ?? []).includes("enrichment_unavailable")) {
        enrichmentUnavailable += 1;
      }
    }
    return { metadataIncomplete, costOmitted, enrichmentUnavailable };
  }, [selectedModels]);
  const normalizedOrigin = useMemo(
    () => normalizeGatewayOrigin(gatewayOrigin),
    [gatewayOrigin],
  );
  const normalizedProviderId = providerId.trim();
  const gatewayOriginInvalid = normalizedOrigin === null;
  const providerIdInvalid =
    normalizedProviderId === "" || normalizedProviderId.includes("/");

  const toggleModel = (id: number, checked: boolean) => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const batchSelectVisible = () => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) {
        if (
          model.selectable &&
          (!priceCompleteOnly || model.price_risk.exportable)
        )
          next.add(model.model_config_id);
      }
      return next;
    });
  };

  const batchClearVisible = () => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) next.delete(model.model_config_id);
      return next;
    });
  };

  const handleFileUpload = async (file: File | undefined) => {
    if (!file) return;
    // Confirmations are scoped to one exact parsed file. In particular, a
    // second file with the same provider/model/header names but new values must
    // never inherit approvals from the first file.
    setConfirmedHeaders({});
    setExtraction(null);
    setExtractionError(null);
    try {
      const text = await file.text();
      const parsed = extractClientConfig(text);
      setExtraction(parsed);
    } catch (error) {
      setExtraction(null);
      setExtractionError(
        error instanceof Error ? error.message : String(error),
      );
    }
  };

  const applyExtraction = () => {
    if (!extraction) return;
    const byModelId = new Map(models.map((m) => [m.model_id, m]));
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
      // unchecking an item must remove the previously applied value instead of
      // silently retaining it in the manual layer.
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
    if (matchedIds.size === 0) {
      setExtractionError(copy.noExtractedMatch);
    } else {
      setExtractionError(null);
    }
  };

  const buildRenderRequest = (decision: KeyDecision) => {
    const ids = [...selectedIds].sort((a, b) => a - b);
    const enhancementWires: Record<number, ManualEnhancementWire | null> = {};
    for (const id of ids) {
      const draft = enhancements[id];
      enhancementWires[id] = draft
        ? { fields: draft.fields, override_fields: draft.overrideFields }
        : null;
    }
    if (!normalizedOrigin || providerIdInvalid) {
      throw new Error("invalid export destination");
    }
    const manualKey = decision.manualKey.trim();
    return {
      expected_source_digest: sourceQuery.data?.source_digest ?? "",
      model_config_ids: ids,
      base_url: normalizedOrigin,
      provider_id: normalizedProviderId,
      enhancements: enhancementWires,
      credential:
        decision.mode === "manual"
          ? { include: true, api_key: manualKey }
          : { include: false },
      ...(platform === "opencode" && defaultModelConfigId !== undefined
        ? { default_model_config_id: defaultModelConfigId }
        : {}),
    };
  };

  const openKeyDialogDisabled =
    selectedCount === 0 ||
    !sourceQuery.data ||
    gatewayOriginInvalid ||
    providerIdInvalid;

  const handleGenerate = async (decision: KeyDecision) => {
    setRenderError(null);
    setRenderStale(false);
    try {
      const response = await renderModelExport(
        buildRenderRequest(decision),
        platform,
      );
      setRenderResult(response);
    } catch (error) {
      const detail = error as {
        status?: number;
        code?: string;
        message?: string;
      };
      if (detail.code === "export_source_stale" || detail.status === 409) {
        setRenderStale(true);
        void sourceQuery.refetch();
      }
      setRenderError(detail.message ?? copy.renderFailed);
      throw error;
    }
  };

  const clearResult = () => {
    setRenderResult(null);
  };

  return (
    <OperatorPageShell>
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button
          onClick={() => setKeyDialogOpen(true)}
          disabled={openKeyDialogDisabled}
        >
          {copy.generateButton}
          {selectedCount > 0 ? ` (${selectedCount})` : ""}
        </Button>
      </OperatorPageHeader>
      <div className="flex flex-col gap-4">
        <OperatorSectionCard
          title={copy.commonSettingsTitle}
          description={copy.commonSettingsDescription}
        >
          <FieldGroup className="gap-4 md:grid md:grid-cols-2 xl:grid-cols-4">
            <Field>
              <FieldLabel htmlFor="export-platform">
                {copy.platformLabel}
              </FieldLabel>
              <Select
                value={platform}
                onValueChange={(value) =>
                  handlePlatformSwitch(value as ExportPlatform)
                }
              >
                <SelectTrigger
                  id="export-platform"
                  aria-label={copy.platformLabel}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="pi">{copy.platformPi}</SelectItem>
                    <SelectItem value="opencode">
                      {copy.platformOpencode}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field data-invalid={gatewayOriginInvalid}>
              <FieldLabel htmlFor="export-gateway-origin">
                {copy.gatewayOriginLabel}
              </FieldLabel>
              <Input
                id="export-gateway-origin"
                value={gatewayOrigin}
                aria-invalid={gatewayOriginInvalid}
                spellCheck={false}
                onChange={(event) => setGatewayOrigin(event.target.value)}
              />
              <FieldDescription>
                {gatewayOriginInvalid
                  ? copy.gatewayOriginInvalid
                  : copy.gatewayOriginHint}
              </FieldDescription>
            </Field>
            <Field data-invalid={providerIdInvalid}>
              <FieldLabel htmlFor="export-provider-id">
                {copy.providerIdLabel}
              </FieldLabel>
              <Input
                id="export-provider-id"
                value={providerId}
                aria-invalid={providerIdInvalid}
                spellCheck={false}
                onChange={(event) => setProviderId(event.target.value)}
              />
              <FieldDescription>
                {providerIdInvalid
                  ? copy.providerIdInvalid
                  : copy.providerIdHint}
              </FieldDescription>
            </Field>
            {platform === "opencode" && (
              <Field>
                <FieldLabel htmlFor="export-default-model">
                  {copy.defaultModelLabel}
                </FieldLabel>
                <Select
                  value={
                    defaultModelConfigId === undefined
                      ? "none"
                      : String(defaultModelConfigId)
                  }
                  onValueChange={(value) =>
                    setSelection((current) => ({
                      ...current,
                      defaultModelConfigId:
                        value === "none" ? undefined : Number(value),
                    }))
                  }
                >
                  <SelectTrigger
                    id="export-default-model"
                    aria-label={copy.defaultModelLabel}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="none">
                        {copy.defaultModelNone}
                      </SelectItem>
                      {selectedModels.map((model) => (
                        <SelectItem
                          key={model.model_config_id}
                          value={String(model.model_config_id)}
                        >
                          {model.model_id}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>{copy.defaultModelHint}</FieldDescription>
              </Field>
            )}
          </FieldGroup>
        </OperatorSectionCard>

        <OperatorSectionCard
          title={copy.selectionTitle}
          actions={
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void sourceQuery.refetch()}
                disabled={sourceQuery.isFetching}
              >
                {sourceQuery.isFetching ? (
                  <Spinner data-icon="inline-start" />
                ) : null}
                {sourceQuery.isFetching
                  ? copy.refreshingSource
                  : copy.refreshSource}
              </Button>
              <Button variant="outline" size="sm" onClick={batchSelectVisible}>
                {copy.batchSelectVisible}
              </Button>
              <Button variant="outline" size="sm" onClick={batchClearVisible}>
                {copy.batchClearVisible}
              </Button>
            </div>
          }
        >
          <FieldGroup className="gap-4 md:grid md:grid-cols-2 md:items-end xl:grid-cols-4">
            <Field>
              <FieldLabel htmlFor="export-model-search">
                {copy.searchLabel}
              </FieldLabel>
              <Input
                id="export-model-search"
                value={searchText}
                onChange={(event) => setSearchText(event.target.value)}
                placeholder={copy.searchPlaceholder}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="export-family-filter">
                {copy.familyFilterLabel}
              </FieldLabel>
              <Select value={familyFilter} onValueChange={setFamilyFilter}>
                <SelectTrigger
                  id="export-family-filter"
                  aria-label={copy.familyFilterLabel}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.familyAll}</SelectItem>
                    <SelectItem value="openai">OpenAI</SelectItem>
                    <SelectItem value="anthropic">Anthropic</SelectItem>
                    <SelectItem value="gemini">Gemini</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="export-metadata-filter">
                {copy.metadataFilterLabel}
              </FieldLabel>
              <Select
                value={metadataFilter}
                onValueChange={(value) =>
                  setMetadataFilter(
                    value as "all" | "complete" | "incomplete",
                  )
                }
              >
                <SelectTrigger
                  id="export-metadata-filter"
                  aria-label={copy.metadataFilterLabel}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.metadataAll}</SelectItem>
                    <SelectItem value="complete">
                      {copy.metadataComplete}
                    </SelectItem>
                    <SelectItem value="incomplete">
                      {copy.metadataIncomplete}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field orientation="horizontal">
              <Switch
                id="export-price-complete"
                checked={priceCompleteOnly}
                onCheckedChange={setPriceCompleteOnly}
              />
              <FieldLabel htmlFor="export-price-complete">
                {copy.priceCompleteOnly}
              </FieldLabel>
            </Field>
          </FieldGroup>
        </OperatorSectionCard>

        <OperatorSectionCard
          title={copy.enhancementTitle}
          description={copy.uploadHint}
        >
          <FieldGroup className="gap-3">
            <Field>
              <FieldLabel htmlFor="export-config-upload">
                {copy.uploadLabel}
              </FieldLabel>
              <Input
                key={platform}
                id="export-config-upload"
                type="file"
                accept=".json,.jsonc,application/json,text/plain"
                onChange={(event) => {
                  const file = event.currentTarget.files?.[0];
                  // The DOM must not retain the uploaded File after this event;
                  // only the sanitized extraction enters component state.
                  event.currentTarget.value = "";
                  void handleFileUpload(file);
                }}
              />
            </Field>
          </FieldGroup>
          {extractionError && (
            <OperatorCallout
              className="mt-3"
              intent="danger"
              description={extractionError}
            />
          )}
          {extraction && (
            <OperatorInsetPanel
              className="mt-3"
              title={copy.extractedSummary
                .replace("{count}", String(extraction.models.length))
                .replace("{kind}", extraction.sourceKind)}
            >
              <div className="flex flex-col gap-3">
                {extraction.headerCandidates.length > 0 ? (
                  <FieldGroup className="gap-2">
                    <FieldDescription>
                      {copy.headerConfirmTitle}
                    </FieldDescription>
                    {extraction.headerCandidates.map((header) => (
                      <Field key={header.id} orientation="horizontal">
                        <Checkbox
                          id={`export-header-${header.id}`}
                          checked={Boolean(confirmedHeaders[header.id])}
                          onCheckedChange={(checked) =>
                            setConfirmedHeaders((current) => ({
                              ...current,
                              [header.id]: checked === true,
                            }))
                          }
                        />
                        <FieldLabel
                          htmlFor={`export-header-${header.id}`}
                          className="min-w-0"
                        >
                          <span className="font-mono">{header.name}</span>
                          <span className="truncate font-mono text-muted-foreground">
                            {header.value}
                          </span>
                        </FieldLabel>
                      </Field>
                    ))}
                  </FieldGroup>
                ) : null}
                {extraction.notes.length > 0 && (
                  <ul className="list-disc pl-5 text-xs text-muted-foreground">
                    {extraction.notes.slice(0, 6).map((note) => (
                      <li key={note}>{note}</li>
                    ))}
                  </ul>
                )}
                <div>
                  <Button size="sm" variant="outline" onClick={applyExtraction}>
                    {copy.applyExtraction}
                  </Button>
                </div>
              </div>
            </OperatorInsetPanel>
          )}
          {Object.keys(enhancements).length > 0 && (
            <p className="mt-3 text-xs text-muted-foreground">
              {copy.enhancedCount.replace(
                "{count}",
                String(Object.keys(enhancements).length),
              )}
            </p>
          )}
        </OperatorSectionCard>

        {sourceQuery.isLoading && (
          <OperatorLoadingState title={copy.loadingSource} />
        )}
        {sourceQuery.isError && (
          <OperatorErrorState
            title={copy.loadFailed}
            description={String(sourceQuery.error)}
            action={
              <OperatorRetryButton onClick={() => void sourceQuery.refetch()}>
                {copy.retry}
              </OperatorRetryButton>
            }
          />
        )}
        {sourceQuery.data && (
          <OperatorTableShell
            summary={copy.modelSummary
              .replace("{visible}", String(visibleModels.length))
              .replace("{selected}", String(selectedCount))}
          >
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.columnSelect}</TableHead>
                    <TableHead>{copy.columnModel}</TableHead>
                    <TableHead>{copy.columnFamily}</TableHead>
                    <TableHead>{copy.columnTargets}</TableHead>
                    <TableHead>{copy.columnMetadata}</TableHead>
                    <TableHead>{copy.columnPrice}</TableHead>
                    <TableHead>{copy.columnRisks}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleModels.map((model) => (
                    <ModelRow
                      key={model.model_config_id}
                      model={model}
                      selected={selectedIds.has(model.model_config_id)}
                      enhanced={Boolean(enhancements[model.model_config_id])}
                      onToggle={(checked) =>
                        toggleModel(model.model_config_id, checked)
                      }
                    />
                  ))}
                  {visibleModels.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={7}
                        className="py-8 text-center text-muted-foreground"
                      >
                        {copy.emptyTable}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
          </OperatorTableShell>
        )}

        {sourceQuery.data && (
          <OperatorInsetPanel title={copy.riskSummaryTitle}>
            <dl className="grid gap-3 text-xs sm:grid-cols-3">
              <div>
                <dt className="text-muted-foreground">
                  {copy.riskMetadataMissing}
                </dt>
                <dd
                  className="font-mono text-base tabular-nums"
                  data-testid="export-risk-metadata-count"
                >
                  {selectedRiskSummary.metadataIncomplete}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">
                  {copy.riskCostOmitted}
                </dt>
                <dd
                  className="font-mono text-base tabular-nums"
                  data-testid="export-risk-cost-count"
                >
                  {selectedRiskSummary.costOmitted}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">
                  {copy.riskEnrichmentUnavailable}
                </dt>
                <dd
                  className="font-mono text-base tabular-nums"
                  data-testid="export-risk-enrichment-count"
                >
                  {selectedRiskSummary.enrichmentUnavailable}
                </dd>
              </div>
            </dl>
            <p className="mt-3 text-xs text-muted-foreground">
              {copy.riskSummaryHint}
            </p>
          </OperatorInsetPanel>
        )}

        {sourceQuery.data && (
          <OperatorInsetPanel title={copy.sourceEvidenceTitle}>
            <dl className="grid gap-2 text-xs sm:grid-cols-[auto_1fr]">
              <dt className="text-muted-foreground">
                {copy.targetVersionLabel}
              </dt>
              <dd className="font-mono">
                {sourceQuery.data.target_version}
              </dd>
              <dt className="text-muted-foreground">{copy.digestLabel}</dt>
              <dd className="min-w-0 break-all font-mono">
                {sourceQuery.data.source_digest}
              </dd>
            </dl>
          </OperatorInsetPanel>
        )}

        {renderStale && (
          <OperatorCallout intent="danger" description={copy.sourceDrifted} />
        )}
        {renderError && !renderStale && (
          <OperatorCallout intent="danger" description={renderError} />
        )}

        <PlatformKeyDialog
          open={keyDialogOpen}
          selectedCount={selectedCount}
          onClose={() => setKeyDialogOpen(false)}
          onConfirm={handleGenerate}
        />

        <ExportResultSheet
          result={renderResult}
          onClose={clearResult}
          platform={platform}
        />
      </div>
    </OperatorPageShell>
  );
}

function ModelRow(props: {
  model: ExportSourceModelRow;
  selected: boolean;
  enhanced: boolean;
  onToggle: (checked: boolean) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage as unknown as Record<string, string>;
  const { model } = props;
  const warningLabels = (codes: string[] | undefined) =>
    (codes ?? []).map(
      (code) => copy[WARNING_LABEL_KEYS[code] ?? "warnGeneric"],
    );
  const warningCodes = Array.from(
    new Set([
      ...(model.price_risk.warning_codes ?? []),
      ...(model.warnings ?? []),
    ]),
  );
  return (
    <tr
      data-testid={`export-row-${model.model_config_id}`}
      className="border-b last:border-b-0"
    >
      <td className="py-2 pr-2">
        <Checkbox
          checked={props.selected}
          disabled={!model.selectable}
          onCheckedChange={(checked) => props.onToggle(checked === true)}
          aria-label={model.model_id}
        />
      </td>
      <td className="py-2 pr-2 font-mono">
        {model.model_id}
        {!model.selectable && (
          <Badge variant="outline" className="ml-2">
            {copy.unselectablePrefix}
            {model.unselectable_reason ? ` ${model.unselectable_reason}` : ""}
          </Badge>
        )}
        {props.enhanced && (
          <Badge variant="secondary" className="ml-2">
            {copy.enhancedBadge}
          </Badge>
        )}
      </td>
      <td className="py-2 pr-2">{model.api_family}</td>
      <td className="py-2 pr-2 tabular-nums">{model.targets.length}</td>
      <td className="py-2 pr-2">
        {model.enrichment.available
          ? copy.metadataFull
          : copy.metadataStoredOnly}
      </td>
      <td className="py-2 pr-2">
        {model.price_risk.exportable ? copy.priceExportable : copy.priceOmitted}
      </td>
      <td className="py-2">
        <div className="flex flex-wrap gap-1">
          {warningCodes.map((code) => (
            <Badge key={code} variant="outline" title={code}>
              {warningLabels([code])[0]}
            </Badge>
          ))}
        </div>
      </td>
    </tr>
  );
}

export default ModelExportPage;
