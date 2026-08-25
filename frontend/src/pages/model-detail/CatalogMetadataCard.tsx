import { useCallback, useEffect, useMemo, useState } from "react";
import { RefreshCw, Undo2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/management";
import type {
  CatalogCandidate,
  ModelCatalogMetadata,
  ModelCatalogRefreshPreviewResponse,
  ModelCatalogResponse,
} from "@/lib/types";
import {
  OperatorInsetPanel,
  OperatorSectionCard,
  OperatorStatusBadge,
} from "@/shared/design-system";

// The metadata fields shown on the card, in stable display order. Each entry
// renders from the effective projection; the same key is addressable in the
// override editor.
type CatalogFieldKey = keyof ModelCatalogMetadata;

const FIELD_ORDER: CatalogFieldKey[] = [
  "name",
  "description",
  "family",
  "release_date",
  "last_updated",
  "knowledge",
  "reasoning",
  "tool_call",
  "structured_output",
  "temperature",
  "attachment",
  "modalities_input",
  "modalities_output",
  "limit_context",
  "limit_input",
  "limit_output",
  "open_weights",
  "status",
];

function renderFieldValue(
  metadata: ModelCatalogMetadata | null,
  key: CatalogFieldKey,
): string | null {
  const value = metadata?.[key];
  if (value === null || value === undefined) return null;
  if (Array.isArray(value)) return value.join("、");
  if (typeof value === "boolean") return value ? "是" : "否";
  return String(value);
}

/**
 * models.dev 目录元信息卡：来源快照 + 人工覆盖 + 生效值，全部管理面展示，
 * 不参与路由。刷新只替换来源值并保留人工覆盖；逐字段恢复通过把该字段的
 * 覆盖写回 null 实现。
 */
export function CatalogMetadataCard({
  modelConfigId,
  catalog,
  onChanged,
}: {
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  onChanged: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bindOpen, setBindOpen] = useState(false);
  const [refreshOpen, setRefreshOpen] = useState(false);
  const [overrideOpen, setOverrideOpen] = useState(false);

  const runAction = useCallback(
    async (action: () => Promise<unknown>, done?: () => void) => {
      setBusy(true);
      setError(null);
      try {
        await action();
        onChanged();
        done?.();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setBusy(false);
      }
    },
    [onChanged],
  );

  const bound = Boolean(catalog?.bound);
  const matchBadgeIntent = !bound
    ? ("idle" as const)
    : catalog?.match_source === "manual"
      ? ("accent" as const)
      : ("healthy" as const);
  const matchLabel = !bound
    ? copy.stateUnbound
    : catalog?.match_source === "manual"
      ? copy.stateManual
      : copy.stateUnique;

  return (
    <OperatorSectionCard
      title={copy.cardTitle}
      description={copy.cardDescription}
      actions={
        <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || !bound}
            onClick={() => setRefreshOpen(true)}
          >
            <RefreshCw data-icon="inline-start" />
            {copy.refreshAction}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => setBindOpen(true)}
          >
            {bound ? copy.rebindAction : copy.bindAction}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={busy || !bound}
            onClick={() => setOverrideOpen(true)}
          >
            {copy.overrideAction}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-[var(--density-inline-gap)]">
        <div className="flex flex-wrap items-center gap-2">
          <OperatorStatusBadge
            intent={matchBadgeIntent}
            preserveLabel
            label={matchLabel}
          />
          {bound && (
            <>
              <span className="font-mono text-sm text-muted-foreground">
                {catalog?.provider_id} / {catalog?.catalog_model_id}
              </span>
              {catalog?.fetched_at && (
                <span className="text-xs text-muted-foreground">
                  {copy.fetchedAtLabel}:{" "}
                  {new Date(catalog.fetched_at).toLocaleString()}
                </span>
              )}
              {catalog?.override &&
                Object.values(catalog.override).some(
                  (value) => value !== null,
                ) && (
                  <OperatorStatusBadge
                    intent="degraded"
                    preserveLabel
                    label={copy.hasOverridesBadge}
                  />
                )}
            </>
          )}
        </div>

        {!bound ? (
          <p className="text-sm text-muted-foreground">{copy.unboundHint}</p>
        ) : (
          <OperatorInsetPanel className="grid gap-x-6 gap-y-1 sm:grid-cols-2 lg:grid-cols-3">
            {FIELD_ORDER.map((key) => {
              const effective = renderFieldValue(
                catalog?.effective ?? null,
                key,
              );
              if (
                effective === null &&
                renderFieldValue(catalog?.source ?? null, key) === null
              ) {
                return null;
              }
              const overridden =
                renderFieldValue(catalog?.override ?? null, key) !== null;
              return (
                <div key={key} className="flex min-w-0 flex-col">
                  <span className="text-xs text-muted-foreground">
                    {fieldLabels(copy, key)}
                    {overridden && (
                      <span className="ml-1 text-warning-foreground">
                        ·{copy.overrideMarker}
                      </span>
                    )}
                  </span>
                  <span
                    className="truncate text-sm font-medium"
                    title={effective ?? undefined}
                  >
                    {effective ?? copy.valueAbsent}
                  </span>
                </div>
              );
            })}
          </OperatorInsetPanel>
        )}

        {error && (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        )}
      </div>

      {/* Dialogs mount only while open: local draft state dies with the
          unmount instead of being reset by an effect. */}
      {bindOpen && (
        <BindDialog
          isOpen
          modelConfigId={modelConfigId}
          busy={busy}
          onClose={() => setBindOpen(false)}
          runAction={runAction}
        />
      )}
      {refreshOpen && (
        <RefreshDialog
          isOpen
          modelConfigId={modelConfigId}
          onClose={() => setRefreshOpen(false)}
          runAction={runAction}
        />
      )}
      {overrideOpen && (
        <OverrideDialog
          modelConfigId={modelConfigId}
          catalog={catalog}
          busy={busy}
          onClose={() => setOverrideOpen(false)}
          runAction={runAction}
        />
      )}
    </OperatorSectionCard>
  );
}

function fieldLabels(
  copy: Messages["modelCatalog"],
  key: CatalogFieldKey,
): string {
  const labels: Record<CatalogFieldKey, string> = {
    name: copy.fieldName,
    description: copy.fieldDescription,
    family: copy.fieldFamily,
    release_date: copy.fieldReleaseDate,
    last_updated: copy.fieldLastUpdated,
    knowledge: copy.fieldKnowledge,
    reasoning: copy.fieldReasoning,
    tool_call: copy.fieldToolCall,
    structured_output: copy.fieldStructuredOutput,
    temperature: copy.fieldTemperature,
    attachment: copy.fieldAttachment,
    modalities_input: copy.fieldModalitiesInput,
    modalities_output: copy.fieldModalitiesOutput,
    limit_context: copy.fieldLimitContext,
    limit_input: copy.fieldLimitInput,
    limit_output: copy.fieldLimitOutput,
    open_weights: copy.fieldOpenWeights,
    status: copy.fieldStatus,
  };
  return labels[key];
}

type Messages = import("@/i18n/messages").Messages;

/** 绑定对话框：唯一精确匹配自动进入可提交预览；歧义/无匹配必须显式坐标。 */
function BindDialog({
  isOpen,
  modelConfigId,
  busy,
  onClose,
  runAction,
}: {
  isOpen: boolean;
  modelConfigId: number;
  busy: boolean;
  onClose: () => void;
  runAction: (
    action: () => Promise<unknown>,
    done?: () => void,
  ) => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [preview, setPreview] = useState<ModelCatalogMatchPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [manualProvider, setManualProvider] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [candidateQuery, setCandidateQuery] = useState("");
  const [candidates, setCandidates] = useState<CatalogCandidate[]>([]);
  const [candidatesTotal, setCandidatesTotal] = useState(0);

  const loadPreview = useCallback(async () => {
    setLoading(true);
    setPreviewError(null);
    try {
      const result = await modelsApi.catalog.matchPreview(modelConfigId);
      setPreview(result);
    } catch (cause) {
      setPreviewError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, [modelConfigId]);

  useEffect(() => {
    void loadPreview();
  }, [loadPreview]);

  // 有界候选查询：手动绑定时的搜索范围覆盖全部 provider。
  useEffect(() => {
    const handle = setTimeout(() => {
      modelsApi.catalog
        .candidates(modelConfigId, {
          q: candidateQuery || undefined,
          scope: candidateQuery ? "all" : "family",
          limit: 20,
        })
        .then((response) => {
          setCandidates(Array.isArray(response.items) ? response.items : []);
          setCandidatesTotal(
            Number.isFinite(response.total) ? response.total : 0,
          );
        })
        .catch(() => {
          setCandidates([]);
        });
    }, 250);
    return () => clearTimeout(handle);
  }, [modelConfigId, candidateQuery]);

  const uniqueMatch = preview?.committable ? preview : null;

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{bindCopyTitle(messages)}</DialogTitle>
          <DialogDescription>{copy.bindDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <p className="text-sm text-muted-foreground">{copy.loadingText}</p>
          )}
          {previewError && (
            <p className="text-sm text-destructive" role="alert">
              {previewError}
            </p>
          )}
          {uniqueMatch && (
            <div className="rounded-md border p-3">
              <p className="text-sm font-medium">{copy.uniqueMatchFound}</p>
              <p className="font-mono text-sm text-muted-foreground">
                {uniqueMatch.provider_id} / {uniqueMatch.catalog_model_id}
              </p>
              <Button
                type="button"
                size="sm"
                className="mt-2"
                disabled={busy}
                onClick={() =>
                  runAction(
                    () =>
                      modelsApi.catalog.bind(modelConfigId, {
                        expected_catalog_revision: uniqueMatch.catalog_revision,
                      }),
                    onClose,
                  )
                }
              >
                {copy.applyUniqueMatch}
              </Button>
            </div>
          )}
          {preview && !uniqueMatch && !loading && (
            <div className="rounded-md border border-warning-foreground/40 bg-warning-background/30 p-3">
              <p className="text-sm font-medium">
                {preview.reason === "ambiguous"
                  ? copy.ambiguousMatch
                  : copy.noMatch}
              </p>
              <ul className="mt-1 list-inside list-disc text-xs text-muted-foreground">
                {preview.candidates.slice(0, 5).map((candidate) => (
                  <li
                    key={`${candidate.provider_id}/${candidate.model_id}`}
                    className="font-mono"
                  >
                    {candidate.provider_id} / {candidate.model_id}
                  </li>
                ))}
              </ul>
              <p className="mt-1 text-xs text-muted-foreground">
                {copy.explicitBindHint}
              </p>
            </div>
          )}

          <div className="flex flex-col gap-2 rounded-md border p-3">
            <p className="text-sm font-medium">{copy.manualBindTitle}</p>
            <div className="grid grid-cols-2 gap-2">
              <div className="flex flex-col gap-1">
                <Label htmlFor="catalog-bind-provider">
                  {copy.providerLabel}
                </Label>
                <Input
                  id="catalog-bind-provider"
                  value={manualProvider}
                  onChange={(event) =>
                    setManualProvider(event.target.value.trim())
                  }
                  placeholder="openai"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="catalog-bind-model">{copy.modelIdLabel}</Label>
                <Input
                  id="catalog-bind-model"
                  value={manualModel}
                  onChange={(event) =>
                    setManualModel(event.target.value.trim())
                  }
                  placeholder="gpt-4o"
                />
              </div>
            </div>
            <Button
              type="button"
              size="sm"
              disabled={busy || !preview || !manualProvider || !manualModel}
              onClick={() =>
                preview &&
                runAction(
                  () =>
                    modelsApi.catalog.bind(modelConfigId, {
                      provider_id: manualProvider,
                      catalog_model_id: manualModel,
                      expected_catalog_revision: preview.catalog_revision,
                    }),
                  onClose,
                )
              }
            >
              {copy.bindExplicitAction}
            </Button>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="catalog-candidate-search">
              {copy.candidateSearchLabel}
            </Label>
            <Input
              id="catalog-candidate-search"
              value={candidateQuery}
              onChange={(event) => setCandidateQuery(event.target.value)}
              placeholder={copy.candidateSearchPlaceholder}
            />
            <ul className="max-h-40 overflow-y-auto text-sm">
              {candidates.map((candidate) => (
                <li key={`${candidate.provider_id}/${candidate.model_id}`}>
                  <button
                    type="button"
                    className="w-full truncate rounded px-1 py-0.5 text-left hover:bg-muted"
                    onClick={() => {
                      setManualProvider(candidate.provider_id);
                      setManualModel(candidate.model_id);
                    }}
                  >
                    <span className="font-mono">
                      {candidate.provider_id}/{candidate.model_id}
                    </span>
                    <span className="ml-2 text-muted-foreground">
                      {candidate.name}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
            <p className="text-xs text-muted-foreground">
              {copy.candidateCount(candidates.length, candidatesTotal)}
            </p>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function bindCopyTitle(messages: Messages): string {
  return messages.modelCatalog.bindDialogTitle;
}

interface ModelCatalogMatchPreview {
  committable: boolean;
  provider_id?: string;
  catalog_model_id?: string;
  candidates: CatalogCandidate[];
  reason: string;
  catalog_revision: string;
  fetched_at: string;
}

/** 刷新差异预览：只替换 source_values，人工覆盖保持不变。 */
function RefreshDialog({
  isOpen,
  modelConfigId,
  onClose,
  runAction,
}: {
  isOpen: boolean;
  modelConfigId: number;
  onClose: () => void;
  runAction: (
    action: () => Promise<unknown>,
    done?: () => void,
  ) => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  // Settled-record pattern: pending derives from "no settled result yet", so
  // no effect body ever calls setState synchronously.
  const [settled, setSettled] = useState<{
    preview: ModelCatalogRefreshPreviewResponse | null;
    error: string | null;
  } | null>(null);
  const loading = settled === null;
  const preview = settled?.preview ?? null;
  const error = settled?.error ?? null;

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const response = await modelsApi.catalog.refreshPreview(modelConfigId);
        if (!cancelled) setSettled({ preview: response, error: null });
      } catch (cause) {
        if (!cancelled)
          setSettled({
            preview: null,
            error: cause instanceof Error ? cause.message : String(cause),
          });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [modelConfigId]);

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.refreshDialogTitle}</DialogTitle>
          <DialogDescription>{copy.refreshDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <p className="text-sm text-muted-foreground">{copy.loadingText}</p>
          )}
          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}
          {preview && (
            <>
              <p className="text-xs text-muted-foreground">
                {copy.refreshRevisionLabel}:{" "}
                <span className="font-mono">{preview.catalog_revision}</span>
              </p>
              {preview.changes.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {copy.refreshNoChanges}
                </p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {preview.changes.map((change) => (
                    <li
                      key={change.field}
                      className="rounded border px-2 py-1 text-sm"
                    >
                      <span className="font-mono text-xs text-muted-foreground">
                        {fieldLabels(copy, change.field as CatalogFieldKey)}
                      </span>
                      <span className="mx-2 line-through opacity-60">
                        {change.current ?? copy.valueAbsent}
                      </span>
                      <span aria-hidden>→</span>
                      <span className="ml-2">
                        {change.next ?? copy.valueAbsent}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            type="button"
            disabled={!preview || loading}
            onClick={() =>
              preview &&
              runAction(
                () =>
                  modelsApi.catalog.refreshCommit(
                    modelConfigId,
                    preview.catalog_revision,
                  ),
                onClose,
              )
            }
          >
            {copy.refreshApply}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const OVERRIDE_TEXT_FIELDS: CatalogFieldKey[] = [
  "name",
  "description",
  "family",
  "release_date",
  "last_updated",
  "knowledge",
  "status",
];

/** 覆盖编辑器：写入人工值或写回 null 恢复来源值。display_name 永不改动。 */
function OverrideDialog({
  modelConfigId,
  catalog,
  busy,
  onClose,
  runAction,
}: {
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  busy: boolean;
  onClose: () => void;
  runAction: (
    action: () => Promise<unknown>,
    done?: () => void,
  ) => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [clearAll, setClearAll] = useState(false);

  const patch = useMemo(() => {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(draft)) {
      if (value === "") {
        result[key] = null;
      } else if (
        key === "limit_context" ||
        key === "limit_input" ||
        key === "limit_output"
      ) {
        const parsed = Number(value);
        if (!Number.isInteger(parsed) || parsed < 0) continue;
        result[key] = parsed;
      } else {
        result[key] = value;
      }
    }
    return result;
  }, [draft]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.overrideDialogTitle}</DialogTitle>
          <DialogDescription>
            {copy.overrideDialogDescription}
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[60vh] flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
          {OVERRIDE_TEXT_FIELDS.map((key) => (
            <div key={key} className="flex items-end gap-2">
              <div className="flex grow flex-col gap-1">
                <Label htmlFor={`override-${key}`}>
                  {fieldLabels(copy, key)}
                </Label>
                <Input
                  id={`override-${key}`}
                  value={draft[key] ?? ""}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      [key]: event.target.value,
                    }))
                  }
                  placeholder={
                    renderFieldValue(catalog?.effective ?? null, key) ??
                    copy.overridePlaceholderSource(
                      renderFieldValue(catalog?.source ?? null, key),
                    )
                  }
                />
              </div>
              {renderFieldValue(catalog?.source ?? null, key) !== null && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  title={copy.restoreFieldTitle}
                  disabled={busy}
                  onClick={() =>
                    runAction(() =>
                      modelsApi.catalog.putOverride(modelConfigId, {
                        [key]: null,
                      }),
                    )
                  }
                >
                  <Undo2 />
                </Button>
              )}
            </div>
          ))}
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={clearAll}
              onCheckedChange={(checked) => setClearAll(checked === true)}
            />
            {copy.clearAllOverridesLabel}
          </label>
          <p className="text-xs text-muted-foreground">
            {copy.overrideDisplayNameNote}
          </p>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          {catalog?.bound && (
            <Button
              type="button"
              variant="destructive"
              disabled={busy}
              onClick={() =>
                runAction(
                  () => modelsApi.catalog.clearOverride(modelConfigId),
                  onClose,
                )
              }
            >
              {copy.clearAllOverridesAction}
            </Button>
          )}
          <Button
            type="button"
            disabled={busy || (Object.keys(patch).length === 0 && !clearAll)}
            onClick={() =>
              clearAll
                ? runAction(
                    () => modelsApi.catalog.clearOverride(modelConfigId),
                    onClose,
                  )
                : runAction(
                    () => modelsApi.catalog.putOverride(modelConfigId, patch),
                    onClose,
                  )
            }
          >
            {copy.saveOverrideAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
