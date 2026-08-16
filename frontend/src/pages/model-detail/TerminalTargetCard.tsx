import { useMemo } from "react"
import { ArrowDown, ArrowUp, Cable, Check, ChevronsUpDown, Copy, CopyPlus, Pencil, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/i18n/useLocale"
import type {
  Connection,
  DiagnosticsTarget,
  LoadbalanceCurrentStateItem,
  ModelAccessTargetMutation,
  OpenAITextCapability,
} from "@/lib/types"
import { OperatorTypeBadge, OperatorStatusBadge } from "@/shared/design-system"
import { classifyOpenAICoverage } from "./classifyOpenAICoverage"
import { TerminalTargetRuntimeSummary } from "./TerminalTargetRuntimeSummary"

interface TerminalTargetCardProps {
  stagePosition: number
  target: ModelAccessTargetMutation
  connection: Connection | null
  diagnosticsTarget: DiagnosticsTarget | null
  truncatedBySingle: boolean
  ownerOpenAIAcceptedFormat: OpenAITextCapability | null | undefined
  isReadOnly: boolean
  canMoveUp: boolean
  canMoveDown: boolean
  busy: boolean
  disabled?: boolean
  runtimeState: LoadbalanceCurrentStateItem | null | undefined
  runtimeResetting: boolean
  runtimeRefreshError?: string | null
  onToggle?: (enabled: boolean) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onEdit?: () => void
  onCopy?: () => void
  onDelete?: () => void
  onQuickCapabilityChange?: (capability: OpenAITextCapability) => void
  pricingTemplates?: Array<{ id: number; name: string }>
  onQuickPricingChange?: (pricingTemplateId: number | null) => void
  onResetCooldown?: (connectionId: number) => void
  onRefreshRuntime?: () => void
}

function capabilityLabel(capability: OpenAITextCapability | null | undefined, copy: { capabilityChatOnly: string; capabilityDual: string; capabilityResponsesOnly: string }) {
  switch (capability) {
    case "chat_completions_only":
      return copy.capabilityChatOnly
    case "responses_only":
      return copy.capabilityResponsesOnly
    default:
      return copy.capabilityDual
  }
}

function coveragePresentation(coverage: string | undefined) {
  switch (coverage) {
    case "full":
      return { intent: "healthy" as const, label: "coverageFull" }
    case "partial":
      return { intent: "degraded" as const, label: "coveragePartial" }
    case "none":
      return { intent: "failing" as const, label: "coverageNone" }
    default:
      return null
  }
}

function getConnectionName(connection: Connection | null, connectionId: number | undefined, fallback: (id: string) => string) {
  if (!connection) return fallback(String(connectionId ?? ""))
  return connection.name?.trim() || connection.endpoint?.name?.trim() || fallback(String(connection.id))
}

// TerminalTargetCard renders the three-layer rich card: identity/capability,
// static configuration, and process-local Ban Policy runtime state. It never
// shows endpoint keys, header values or request-parameter values.
export function TerminalTargetCard({
  stagePosition,
  target,
  connection,
  diagnosticsTarget,
  truncatedBySingle,
  ownerOpenAIAcceptedFormat,
  isReadOnly,
  canMoveUp,
  canMoveDown,
  busy,
  disabled = false,
  runtimeState,
  runtimeResetting,
  runtimeRefreshError,
  onToggle,
  onMoveUp,
  onMoveDown,
  onEdit,
  onCopy,
  onDelete,
  onQuickCapabilityChange,
  pricingTemplates = [],
  onQuickPricingChange,
  onResetCooldown,
  onRefreshRuntime,
}: TerminalTargetCardProps) {
  const { messages } = useLocale()
  const copy = messages.routing
  const detailCopy = messages.modelDetail
  const connectionId = target.connection_id ?? connection?.id
  const connectionName = getConnectionName(connection, connectionId, messages.modelDetailData.connectionFallback)
  const capability = connection?.openai_text_capability ?? null
  const coverage = diagnosticsTarget?.coverage
  const coverageBadge = coveragePresentation(coverage)
  const directPreview = useMemo(
    () =>
      ownerOpenAIAcceptedFormat && capability
        ? classifyOpenAICoverage(ownerOpenAIAcceptedFormat, capability)
        : null,
    [ownerOpenAIAcceptedFormat, capability],
  )
  const missingOperations = directPreview?.unsupportedAcceptedOperations ?? diagnosticsTarget?.unsupported_accepted_operations ?? []
  const endpoint = connection?.endpoint ?? null
  const pricingTemplate = connection?.pricing_template ?? null

  return (
    <div
      data-testid={`terminal-target-card-${connectionId ?? "unknown"}`}
      className="flex flex-col gap-3 rounded-md border bg-background px-3 py-3"
      tabIndex={-1}
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <div className="flex size-[var(--density-control-h-sm)] shrink-0 items-center justify-center rounded-md border border-border bg-inset text-muted-foreground">
            <Cable />
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{connectionName}</p>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <OperatorTypeBadge intent="muted" label={`${copy.connectionTarget} · ${copy.stagePosition(String(stagePosition))}`} preserveLabel />
              <OperatorTypeBadge intent="muted" label={capabilityLabel(capability, copy)} preserveLabel />
              {coverageBadge ? (
                <OperatorStatusBadge intent={coverageBadge.intent} label={copy[coverageBadge.label as keyof typeof copy] as string} preserveLabel />
              ) : null}
              {truncatedBySingle ? (
                <OperatorStatusBadge intent="degraded" label={copy.truncatedBySingle} preserveLabel />
              ) : null}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                <Check data-icon="inline-start" className="size-3" />
                {target.is_enabled !== false ? copy.participatesInRouting : detailCopy.disabled}
              </span>
              <span className="inline-flex items-center gap-1">
                {connection?.is_active === false ? copy.connectionInactive : copy.connectionActive}
              </span>
              {missingOperations.length > 0 ? (
                <span className="text-degraded">{copy.missingOperations(missingOperations.join("、"))}</span>
              ) : null}
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2">
          {!isReadOnly ? (
            <Switch
              checked={target.is_enabled !== false}
              disabled={disabled || busy}
              onCheckedChange={(checked) => onToggle?.(checked)}
              aria-label={copy.enableAccessTarget(copy.stagePosition(String(stagePosition)))}
            />
          ) : null}
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            aria-label={copy.targetMoveUp(connectionName)}
            disabled={disabled || busy || !canMoveUp}
            onClick={onMoveUp}
          >
            <ArrowUp />
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            aria-label={copy.targetMoveDown(connectionName)}
            disabled={disabled || busy || !canMoveDown}
            onClick={onMoveDown}
          >
            <ArrowDown />
          </Button>
          {onEdit ? (
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              aria-label={`${detailCopy.edit} ${connectionName}`}
              disabled={disabled || busy}
              onClick={onEdit}
            >
              <Pencil />
            </Button>
          ) : null}
          {onCopy ? (
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              aria-label={copy.copyTargetToOtherModels(connectionName)}
              disabled={disabled || busy}
              onClick={onCopy}
            >
              <CopyPlus />
            </Button>
          ) : null}
          {onQuickCapabilityChange && !isReadOnly ? (
            <label className="relative inline-flex">
              <span className="sr-only">{copy.quickCapabilityLabel(connectionName)}</span>
              <Select
                value={capability ?? "dual_native"}
                onValueChange={(value) => onQuickCapabilityChange(value as OpenAITextCapability)}
                disabled={disabled || busy}
              >
                <SelectTrigger className="h-8 w-auto gap-1 px-2 text-xs" aria-label={copy.quickCapabilityLabel(connectionName)}>
                  <ChevronsUpDown className="size-3" />
                  <span className="max-w-28 truncate">{capabilityLabel(capability, copy)}</span>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {(["dual_native", "chat_completions_only", "responses_only"] as OpenAITextCapability[]).map((option) => {
                      const preview = ownerOpenAIAcceptedFormat ? classifyOpenAICoverage(ownerOpenAIAcceptedFormat, option) : null
                      const noneDisabled = preview?.coverage === "none"
                      return (
                        <SelectItem key={option} value={option} disabled={noneDisabled}>
                          {capabilityLabel(option, copy)}
                          {preview ? ` · ${copy[preview.coverage === "full" ? "coverageFull" : preview.coverage === "partial" ? "coveragePartial" : "coverageNone"]}` : ""}
                        </SelectItem>
                      )
                    })}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </label>
          ) : null}
          {onQuickPricingChange && !isReadOnly ? (
            <label className="relative inline-flex">
              <span className="sr-only">{copy.quickPricingLabel(connectionName)}</span>
              <Select
                value={pricingTemplate ? String(pricingTemplate.id) : "none"}
                onValueChange={(value) => onQuickPricingChange(value === "none" ? null : Number.parseInt(value, 10))}
                disabled={disabled || busy}
              >
                <SelectTrigger className="h-8 w-auto gap-1 px-2 text-xs" aria-label={copy.quickPricingLabel(connectionName)}>
                  <ChevronsUpDown className="size-3" />
                  <span className="max-w-28 truncate">{pricingTemplate ? pricingTemplate.name : copy.noPricingTemplate}</span>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="none">{copy.noPricingTemplate}</SelectItem>
                    {pricingTemplates.map((template) => (
                      <SelectItem key={template.id} value={String(template.id)}>{template.name}</SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </label>
          ) : null}
          {!isReadOnly && onDelete ? (
            <Button
              type="button"
              variant="outline"
              size="icon-sm"
              aria-label={copy.targetRemove(connectionName)}
              disabled={disabled || busy}
              onClick={onDelete}
            >
              <Trash2 />
            </Button>
          ) : null}
        </div>
      </div>

      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs text-muted-foreground sm:grid-cols-2">
        {endpoint ? (
          <div className="flex min-w-0 items-center gap-1">
            <dt className="shrink-0">{copy.endpointBaseUrl}:</dt>
            <dd className="min-w-0 truncate font-mono" title={endpoint.base_url}>
              {endpoint.base_url}
            </dd>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={copy.copyBaseUrl}
              onClick={() => void navigator.clipboard?.writeText(endpoint.base_url)}
            >
              <Copy />
            </Button>
          </div>
        ) : null}
        <div className="flex items-center gap-1">
          <dt className="shrink-0">{copy.pricingTemplate}:</dt>
          <dd className="min-w-0 truncate">{pricingTemplate ? pricingTemplate.name : copy.noPricingTemplate}</dd>
        </div>
        <div className="flex items-center gap-1">
          <dt className="shrink-0">{copy.limits}:</dt>
          <dd className="font-mono tabular-nums">
            {copy.qpsLimit(connection?.qps_limit != null ? String(connection.qps_limit) : copy.unlimited)} ·{" "}
            {copy.inFlightLimits(
              connection?.max_in_flight_non_stream != null ? String(connection.max_in_flight_non_stream) : copy.unlimited,
              connection?.max_in_flight_stream != null ? String(connection.max_in_flight_stream) : copy.unlimited,
            )}
          </dd>
        </div>
        <div className="flex items-center gap-1">
          <dt className="shrink-0">{copy.customHeaders}:</dt>
          <dd className="font-mono tabular-nums">{connection?.custom_headers ? String(Object.keys(connection.custom_headers).length) : "0"}</dd>
        </div>
      </dl>

      <TerminalTargetRuntimeSummary
        connectionId={connectionId ?? 0}
        state={runtimeState}
        resetting={runtimeResetting}
        onResetCooldown={onResetCooldown}
        onRefresh={onRefreshRuntime}
        refreshError={runtimeRefreshError}
      />
    </div>
  )
}
