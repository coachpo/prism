import { zodResolver } from "@hookform/resolvers/zod"
import { useCallback, useEffect, useRef, useState } from "react"
import { useForm, useWatch, type Resolver, type UseFormRegisterReturn } from "react-hook-form"
import { Loader2, Sparkles } from "lucide-react"
import type { LoadbalanceStrategy, StrategyPreviewResponse } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldSet, FieldLegend } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/i18n/useLocale"
import { api } from "@/lib/api"
import { OperatorCallout, OperatorLoadingState, OperatorTypeBadge } from "@/shared/design-system"
import { BAN_POLICY_PRESETS, BAN_POLICY_PRESET_ORDER, banPolicyFormSchema, banPolicyModes, banPolicyRoutingTypes, buildBanPolicyPreviewPayload, presetMatchingValues, type BanPolicyFormValues, type BanPolicyPresetKey, type ProvenanceMap } from "./banPolicySchemas"

interface BanPolicyDialogProps {
  editingStrategy: LoadbalanceStrategy | null
  initialValues: BanPolicyFormValues
  open: boolean
  saving: boolean
  saveError: string | null
  onClose: () => void
  onOpenChange: (open: boolean) => void
  onSave: (values: BanPolicyFormValues) => Promise<void>
}

type PreviewState =
  | { phase: "idle" }
  | { phase: "loading"; generation: number }
  | { phase: "ready"; data: StrategyPreviewResponse; generation: number }
  | { phase: "error"; message: string; generation: number }

export function BanPolicyDialog({ editingStrategy, initialValues, open, saving, saveError, onClose, onOpenChange, onSave }: BanPolicyDialogProps) {
  const { messages } = useLocale()
  const copy = messages.routingStrategyDialog
  const strategyCopy = messages.loadbalanceStrategyCopy
  const [provenance, setProvenance] = useState<ProvenanceMap>({})
  const [presetConfirmKey, setPresetConfirmKey] = useState<BanPolicyPresetKey | null>(null)
  const [preview, setPreview] = useState<PreviewState>({ phase: "idle" })
  const previewGeneration = useRef(0)
  const liveRegionRef = useRef<HTMLParagraphElement>(null)

  const form = useForm<BanPolicyFormValues>({
    resolver: zodResolver(banPolicyFormSchema) as unknown as Resolver<BanPolicyFormValues>,
    defaultValues: initialValues,
    mode: "onChange",
  })
  const banMode = useWatch({ control: form.control, name: "ban_mode" })
  const strategyType = useWatch({ control: form.control, name: "legacy_strategy_type" })
  const cycleRetryAttemptLimit = useWatch({ control: form.control, name: "cycle_retry_attempt_limit" })
  const threshold = useWatch({ control: form.control, name: "ban_cumulative_retry_attempt_threshold" })
  const duration = useWatch({ control: form.control, name: "ban_duration_seconds" })

  useEffect(() => {
    if (open) {
      form.reset(initialValues)
    }
  }, [form, initialValues, open])

  // Reset derived dialog state when the dialog opens; the reset itself happens
  // during the render of the newly opened dialog, not inside the effect.
  const dialogSessionKey = open ? "open" : "closed"
  const [sessionKey, setSessionKey] = useState(dialogSessionKey)
  if (sessionKey !== dialogSessionKey) {
    setSessionKey(dialogSessionKey)
    setProvenance({})
    setPresetConfirmKey(null)
    setPreview({ phase: "idle" })
  }

  const recordChange = useCallback((field: keyof BanPolicyFormValues, from: string, to: string, reason: string) => {
    setProvenance((current) => ({ ...current, [field]: { origin: "system", change: { from, to, reason } } }))
    liveRegionRef.current?.replaceChildren(`${provenanceFieldLabel(field, copy)}：${from} → ${to}（${reason}）`)
  }, [copy])

  const announce = useCallback((text: string) => {
    if (liveRegionRef.current) {
      liveRegionRef.current.textContent = text
    }
  }, [])

  const setWithProvenance = useCallback((field: keyof BanPolicyFormValues, value: unknown, from: string, to: string, reason: string) => {
    form.setValue(field, value as never, { shouldDirty: true, shouldValidate: true })
    recordChange(field, from, to, reason)
    announce(`${field} ${from} → ${to}（${reason}）`)
  }, [announce, form, recordChange])

  // Auto-linkage with provenance (SPEC §10.2): off->temporary|until_reset
  // fills the derived threshold; temporary gets the safe 900s suggestion;
  // until_reset submits duration 0 while keeping the draft.
  const updateBanMode = (value: BanPolicyFormValues["ban_mode"]) => {
    const current = form.getValues()
    const from = current.ban_mode
    form.setValue("ban_mode", value, { shouldDirty: true, shouldValidate: true })
    if (value === "off") {
      if (current.ban_cumulative_retry_attempt_threshold !== 0) {
        setWithProvenance("ban_cumulative_retry_attempt_threshold", 0, String(current.ban_cumulative_retry_attempt_threshold), "0", "封禁关闭时累计阈值必须为 0")
      }
      if (current.ban_duration_seconds !== 0) {
        setWithProvenance("ban_duration_seconds", 0, String(current.ban_duration_seconds), "0", "封禁关闭或直到重置时时长提交 0")
      }
    } else if (from === "off" && current.ban_cumulative_retry_attempt_threshold === 0) {
      const suggested = current.cycle_retry_attempt_limit * 2
      setWithProvenance("ban_cumulative_retry_attempt_threshold", suggested, "0", String(suggested), "从关闭切换到封禁时按每轮上限推导")
    }
    if (value === "temporary") {
      const userSet = provenance.ban_duration_seconds?.origin === "user" || (provenance.ban_duration_seconds?.origin === "preset")
      if (!userSet && (current.ban_duration_seconds < 1 || provenance.ban_duration_seconds?.change)) {
        setWithProvenance("ban_duration_seconds", 900, String(current.ban_duration_seconds), "900", "临时封禁建议时长（后端合法下限为 1 秒）")
      }
    }
    if (value === "until_reset" && current.ban_duration_seconds !== 0) {
      setWithProvenance("ban_duration_seconds", 0, String(current.ban_duration_seconds), "0", "直到重置时提交时长 0")
    }
    announce(`封禁模式已切换为 ${value}`)
  }

  // User-edited thresholds are never silently overwritten by a higher cycle
  // limit; the user sees validation plus a one-click sync action.
  const updateCycleLimit = (rawValue: string) => {
    const nextLimit = Number(rawValue)
    const resolved = Number.isFinite(nextLimit) ? Math.trunc(nextLimit) : 1
    form.setValue("cycle_retry_attempt_limit", resolved, { shouldDirty: true, shouldValidate: true })
    const currentThreshold = form.getValues("ban_cumulative_retry_attempt_threshold")
    const thresholdTouched = form.formState.dirtyFields.ban_cumulative_retry_attempt_threshold
    if (currentThreshold !== 0 && currentThreshold < resolved && !thresholdTouched) {
      setWithProvenance("ban_cumulative_retry_attempt_threshold", resolved, String(currentThreshold), String(resolved), "累计阈值低于新的每轮上限，已同步")
    }
  }

  const syncThresholdToLimit = () => {
    const current = form.getValues()
    setWithProvenance("ban_cumulative_retry_attempt_threshold", current.cycle_retry_attempt_limit, String(current.ban_cumulative_retry_attempt_threshold), String(current.cycle_retry_attempt_limit), "一键同步为每轮上限")
  }

  const applyPreset = (key: BanPolicyPresetKey) => {
    const preset = BAN_POLICY_PRESETS[key]
    const retryOrBanDirty = Boolean(form.formState.dirtyFields.retry_base_delay_ms || form.formState.dirtyFields.retry_max_delay_ms || form.formState.dirtyFields.cycle_retry_attempt_limit || form.formState.dirtyFields.ban_mode || form.formState.dirtyFields.ban_cumulative_retry_attempt_threshold || form.formState.dirtyFields.ban_duration_seconds)
    if (retryOrBanDirty && presetConfirmKey !== key) {
      setPresetConfirmKey(key)
      return
    }
    setPresetConfirmKey(null)
    form.setValue("failure_status_codes_input", preset.failure_status_codes.join(", "), { shouldDirty: true, shouldValidate: true })
    form.setValue("ban_mode", preset.ban_mode, { shouldDirty: true, shouldValidate: true })
    form.setValue("retry_base_delay_ms", preset.retry_base_delay_ms, { shouldDirty: true, shouldValidate: true })
    form.setValue("retry_backoff_multiplier", preset.retry_backoff_multiplier, { shouldDirty: true, shouldValidate: true })
    form.setValue("retry_jitter_ratio", preset.retry_jitter_ratio, { shouldDirty: true, shouldValidate: true })
    form.setValue("retry_max_delay_ms", preset.retry_max_delay_ms, { shouldDirty: true, shouldValidate: true })
    form.setValue("cycle_retry_attempt_limit", preset.cycle_retry_attempt_limit, { shouldDirty: true, shouldValidate: true })
    form.setValue("ban_cumulative_retry_attempt_threshold", preset.ban_cumulative_retry_attempt_threshold, { shouldDirty: true, shouldValidate: true })
    form.setValue("ban_duration_seconds", preset.ban_duration_seconds, { shouldDirty: true, shouldValidate: true })
    setProvenance({
      failure_status_codes_input: { origin: "preset", change: null },
      ban_mode: { origin: "preset", change: null },
      retry_base_delay_ms: { origin: "preset", change: null },
      retry_backoff_multiplier: { origin: "preset", change: null },
      retry_jitter_ratio: { origin: "preset", change: null },
      retry_max_delay_ms: { origin: "preset", change: null },
      cycle_retry_attempt_limit: { origin: "preset", change: null },
      ban_cumulative_retry_attempt_threshold: { origin: "preset", change: null },
      ban_duration_seconds: { origin: "preset", change: null },
    })
    announce(`已应用${presetLabel(key, messages.routingStrategyDialog)}预设`)
    void runPreview()
  }

  // Debounced, side-effect-free preview from the shared backend calculator.
  const runPreview = useCallback(async () => {
    const generation = ++previewGeneration.current
    setPreview({ phase: "loading", generation })
    try {
      const payload = buildBanPolicyPreviewPayload(form.getValues())
      const response = await api.loadbalanceStrategies.preview(payload)
      if (generation !== previewGeneration.current) return
      setPreview({ phase: "ready", data: response, generation })
    } catch (error) {
      if (generation !== previewGeneration.current) return
      setPreview({ phase: "error", message: error instanceof Error ? error.message : "预览计算失败", generation })
    }
  }, [form])

  useEffect(() => {
    if (!open) return
    const timeout = window.setTimeout(() => { void runPreview() }, 400)
    return () => window.clearTimeout(timeout)
  }, [open, banMode, cycleRetryAttemptLimit, threshold, duration, runPreview])

  const handleSubmit = form.handleSubmit((values) => onSave(values))
  const attachedModelCount = editingStrategy?.attached_model_count ?? null
  const activePresetKey = presetMatchingValues(form.getValues())

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (nextOpen) onOpenChange(true); else { setPresetConfirmKey(null); onClose() } }}>
      <DialogContent className="flex h-[min(94vh,58rem)] max-h-[94vh] max-w-4xl flex-col overflow-hidden p-0">
        <DialogHeader className="shrink-0 border-b border-border bg-panel px-6 py-5">
          <DialogTitle>{editingStrategy ? copy.editTitle : copy.addTitle}</DialogTitle>
          <DialogDescription>{copy.description}</DialogDescription>
        </DialogHeader>
        <p ref={liveRegionRef} className="sr-only" role="status" aria-live="polite" />
        <form onSubmit={(event) => { void handleSubmit(event) }} className="flex min-h-0 flex-1 flex-col">
          <DialogBody className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
            <FieldGroup className="gap-6">
              {attachedModelCount === null && editingStrategy ? (
                <OperatorCallout intent="warning" title={messages.routingStrategyTable.editImpactUnknown} />
              ) : null}
              {(attachedModelCount ?? 0) > 0 ? (
                <OperatorCallout intent="warning" title={messages.routingStrategyTable.editImpactCallout(attachedModelCount ?? 0)}>
                  {editingStrategy?.is_default ? messages.routingStrategyTable.defaultOnlyAffectsNewModels : null}
                </OperatorCallout>
              ) : null}
              {saveError ? <OperatorCallout intent="danger" title={copy.saveFailed}>{saveError}</OperatorCallout> : null}

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.groupRouting}</FieldLegend>
                <div className="flex flex-col gap-4">
                  <Field data-invalid={Boolean(form.formState.errors.name)}>
                    <FieldLabel htmlFor="strategy-name">{copy.nameLabel}</FieldLabel>
                    <Input id="strategy-name" placeholder={copy.namePlaceholder} {...registerField(form, "name")} />
                    {form.formState.errors.name ? <FieldError>{form.formState.errors.name.message}</FieldError> : null}
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="strategy-type">{copy.routingTypeLabel}</FieldLabel>
                    <Select value={strategyType} onValueChange={(value) => form.setValue("legacy_strategy_type", value as BanPolicyFormValues["legacy_strategy_type"], { shouldDirty: true })}>
                      <SelectTrigger id="strategy-type" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {banPolicyRoutingTypes.map((type) => (
                            <SelectItem key={type} value={type}>{strategyCopy[strategyLabelKey(type)]}</SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FieldDescription>{strategyCopy[strategySummaryKey(strategyType)]}</FieldDescription>
                  </Field>
                </div>
              </FieldSet>

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.groupFailure}</FieldLegend>
                <Field data-invalid={Boolean(form.formState.errors.failure_status_codes_input)}>
                  <FieldLabel htmlFor="strategy-status-codes">{copy.failureStatusCodesLabel}</FieldLabel>
                  <Textarea id="strategy-status-codes" rows={2} {...registerField(form, "failure_status_codes_input")} />
                  <FieldDescription>{copy.failureStatusCodesDescription}</FieldDescription>
                  {form.formState.errors.failure_status_codes_input ? <FieldError>{form.formState.errors.failure_status_codes_input.message}</FieldError> : null}
                </Field>
              </FieldSet>

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.groupRetry}</FieldLegend>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <RetryField form={form} name="retry_base_delay_ms" label={copy.baseDelayLabel} description={copy.baseDelayDescription} register={registerField} />
                  <RetryField form={form} name="retry_backoff_multiplier" label={copy.multiplierLabel} description={copy.multiplierDescription} register={registerField} />
                  <RetryField form={form} name="retry_jitter_ratio" label={copy.jitterLabel} description={copy.jitterDescription} register={registerField} />
                  <RetryField form={form} name="retry_max_delay_ms" label={copy.maxDelayLabel} description={copy.maxDelayDescription} register={registerField} />
                  <RetryField form={form} name="cycle_retry_attempt_limit" label={copy.cycleLimitLabel} description={copy.cycleLimitDescription} register={registerField} onChange={updateCycleLimit} />
                </div>
              </FieldSet>

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.groupBan}</FieldLegend>
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="strategy-ban-mode">{copy.banModeLabel}</FieldLabel>
                    <Select value={banMode} onValueChange={updateBanMode}>
                      <SelectTrigger id="strategy-ban-mode" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {banPolicyModes.map((mode) => (
                            <SelectItem key={mode} value={mode}>{copy[banModeKey(mode)]}</SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  <RetryField form={form} name="ban_cumulative_retry_attempt_threshold" label={copy.thresholdLabel} description={copy.thresholdDescription} register={registerField} />
                  <RetryField form={form} name="ban_duration_seconds" label={copy.durationLabel} description={copy.durationDescription} register={registerField} />
                </div>
                {form.formState.errors.ban_cumulative_retry_attempt_threshold ? (
                  <div className="flex flex-col gap-2 pt-2">
                    <FieldError>{form.formState.errors.ban_cumulative_retry_attempt_threshold.message}</FieldError>
                    <Button type="button" variant="outline" size="sm" onClick={syncThresholdToLimit}>
                      {copy.syncThresholdToLimit}
                    </Button>
                  </div>
                ) : null}
                {threshold !== 0 && threshold < cycleRetryAttemptLimit ? (
                  <Button type="button" variant="outline" size="sm" className="mt-2" onClick={syncThresholdToLimit}>
                    {copy.syncThresholdToLimitAction(cycleRetryAttemptLimit)}
                  </Button>
                ) : null}
              </FieldSet>

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.presetsLabel}</FieldLegend>
                <div className="flex flex-wrap gap-2">
                  {BAN_POLICY_PRESET_ORDER.map((key) => (
                      <Button key={key} type="button" variant={activePresetKey === key ? "default" : "outline"} size="sm" onClick={() => applyPreset(key)} aria-pressed={activePresetKey === key}>
                        <Sparkles data-icon="inline-start" />
                        {presetLabel(key, copy)}
                      </Button>
                    ))}
                </div>
                {activePresetKey === "aggressive" ? (
                  <OperatorCallout intent="warning" title={copy.presetAggressiveWarning} className="mt-2" />
                ) : null}
                {activePresetKey === null && (banMode !== "off" || cycleRetryAttemptLimit !== 3) ? (
                  <p className="mt-2 text-xs text-foreground/60">{copy.presetCustomLabel("均衡")}</p>
                ) : null}
                {presetConfirmKey ? (
                  <OperatorCallout intent="warning" title={copy.presetReplaceConfirm} className="mt-2">
                    {copy.presetReplaceConfirmDescription}
                    <div className="mt-2 flex gap-2">
                      <Button type="button" variant="default" size="sm" onClick={() => applyPreset(presetConfirmKey)}>{copy.continue}</Button>
                      <Button type="button" variant="outline" size="sm" onClick={() => setPresetConfirmKey(null)}>{copy.cancelApply}</Button>
                    </div>
                  </OperatorCallout>
                ) : null}
              </FieldSet>

              {Object.entries(provenance).length > 0 ? (
                <div className="flex flex-col gap-2 rounded-lg border border-border bg-inset p-4">
                  <span className="text-sm font-medium">{copy.provenanceSystemAdjusted}</span>
                  {Object.entries(provenance).map(([field, record]) => (
                    record?.change ? (
                      <p key={field} className="text-sm text-foreground/70">
                        {copy.provenanceChange(provenanceFieldLabel(field as keyof BanPolicyFormValues, copy), record.change.from, record.change.to, record.change.reason)}
                      </p>
                    ) : null
                  ))}
                </div>
              ) : null}

              <FieldSet className="rounded-lg border border-border p-4">
                <FieldLegend>{copy.groupPreview}</FieldLegend>
                <PreviewPanel preview={preview} onRetry={() => void runPreview()} />
              </FieldSet>
            </FieldGroup>
          </DialogBody>
          <DialogFooter className="shrink-0 border-t border-border px-6 py-4">
            <Button type="button" variant="outline" onClick={() => { setPresetConfirmKey(null); onClose() }}>{copy.cancel}</Button>
            <Button type="submit" disabled={saving || !form.formState.isValid} aria-busy={saving}>
              {saving ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
              {saving ? copy.saving : copy.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function provenanceFieldLabel(field: keyof BanPolicyFormValues, copy: ReturnType<typeof useLocale>["messages"]["routingStrategyDialog"]): string {
  switch (field) {
    case "ban_cumulative_retry_attempt_threshold":
      return copy.thresholdLabel
    case "ban_duration_seconds":
      return copy.durationLabel
    case "cycle_retry_attempt_limit":
      return copy.cycleLimitLabel
    case "retry_base_delay_ms":
      return copy.baseDelayLabel
    case "retry_max_delay_ms":
      return copy.maxDelayLabel
    default:
      return field
  }
}

type StrategyType = BanPolicyFormValues["legacy_strategy_type"]

function strategyLabelKey(type: StrategyType): "singleLabel" | "fillFirstLabel" | "roundRobinLabel" {
  switch (type) {
    case "single":
      return "singleLabel"
    case "fill-first":
      return "fillFirstLabel"
    case "round-robin":
      return "roundRobinLabel"
  }
}

function strategySummaryKey(type: StrategyType): "singleSummary" | "fillFirstSummary" | "roundRobinSummary" {
  switch (type) {
    case "single":
      return "singleSummary"
    case "fill-first":
      return "fillFirstSummary"
    case "round-robin":
      return "roundRobinSummary"
  }
}

function banModeKey(mode: BanPolicyFormValues["ban_mode"]): "banModeOff" | "banModeTemporary" | "banModeUntilReset" {
  switch (mode) {
    case "off":
      return "banModeOff"
    case "temporary":
      return "banModeTemporary"
    case "until_reset":
      return "banModeUntilReset"
  }
}

function presetLabel(key: BanPolicyPresetKey, copy: ReturnType<typeof useLocale>["messages"]["routingStrategyDialog"]): string {
  switch (key) {
    case "conservative":
      return copy.presetConservative
    case "balanced":
      return copy.presetBalanced
    case "aggressive":
      return copy.presetAggressive
  }
}

function registerField(form: ReturnType<typeof useForm<BanPolicyFormValues>>, name: keyof BanPolicyFormValues): UseFormRegisterReturn {
  const { onChange, onBlur, name: fieldName, ref } = form.register(name)
  return { onChange, onBlur, name: fieldName, ref }
}

function RetryField({ form, name, label, description, register, onChange }: {
  form: ReturnType<typeof useForm<BanPolicyFormValues>>
  name: keyof BanPolicyFormValues
  label: string
  description: string
  register: (form: ReturnType<typeof useForm<BanPolicyFormValues>>, name: keyof BanPolicyFormValues) => UseFormRegisterReturn
  onChange?: (value: string) => void
}) {
  const error = form.formState.errors[name]
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={`strategy-${name}`}>{label}</FieldLabel>
      <Input
        id={`strategy-${name}`}
        type="number"
        step="any"
        {...(onChange
          ? { onChange: (event: React.ChangeEvent<HTMLInputElement>) => { void register(form, name).onChange(event); onChange(event.target.value) } }
          : register(form, name))}
      />
      <FieldDescription>{description}</FieldDescription>
      {error ? <FieldError>{error.message}</FieldError> : null}
    </Field>
  )
}

function PreviewPanel({ preview, onRetry }: {
  preview: PreviewState
  onRetry: () => void
}) {
  const { messages } = useLocale()
  const copy = messages.routingStrategyDialog
  if (preview.phase === "loading") {
    return (
      <div className="flex flex-col gap-2">
        <OperatorLoadingState title={copy.previewLoading} description={copy.previewDescription} />
        <PreviewDetails copy={copy} label={messages.common.moreDetails} />
      </div>
    )
  }
  if (preview.phase === "error") {
    return (
      <OperatorCallout intent="danger" title={copy.previewFailed}>
        {preview.message}
        <Button type="button" variant="outline" size="sm" className="mt-2" onClick={onRetry}>{copy.previewFailedRetry}</Button>
      </OperatorCallout>
    )
  }
  if (preview.phase === "idle") {
    return (
      <div className="flex flex-col gap-2">
        <p className="text-sm text-foreground/60">{copy.previewDescription}</p>
        <PreviewDetails copy={copy} label={messages.common.moreDetails} />
      </div>
    )
  }
  const data = preview.data
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <OperatorTypeBadge label={copy.previewRetryCycleBadge} intent="accent" />
        <span className="text-xs text-foreground/60">{copy.previewDescription}</span>
      </div>
      <PreviewDetails copy={copy} label={messages.common.moreDetails} />
      <ol className="flex flex-col gap-2">
        {data.steps.map((step) => (
          <li key={step.failure_ordinal} className="flex flex-col gap-1 rounded-lg border border-border bg-panel px-3 py-2 text-sm">
            <span className="flex flex-wrap items-center gap-2 font-medium">
              {copy.previewStep(step.failure_ordinal)}
              {step.cycle_exhausted ? <OperatorTypeBadge label={copy.previewCycleExhausted} intent="degraded" /> : null}
              {step.ban_transition ? <OperatorTypeBadge label={copy.previewBanTransition(step.ban_transition.mode, step.ban_transition.duration_seconds)} intent="danger" /> : null}
            </span>
            <span className="text-foreground/70">{copy.previewNominal(step.nominal_delay_ms)}</span>
            <span className="text-foreground/70">{copy.previewJitterRange(step.jitter_min_delay_ms, step.jitter_max_delay_ms)}</span>
          </li>
        ))}
      </ol>
      {data.has_more ? <p className="text-sm text-foreground/60">{copy.previewHasMore}</p> : null}
      <p className="text-sm text-foreground/70">
        {terminationCopy(data.termination_reason, data.cycle_exhaustion_after_attempt, copy)}
      </p>
      <p className="text-sm text-foreground/70">
        {copy.previewBanProjection(data.ban_projection.mode, data.ban_projection.cumulative_retry_attempt_threshold)}
      </p>
      <p className="text-xs text-foreground/60">{copy.previewValidationNote}</p>
      {data.ban_projection.mode === "temporary" || data.ban_projection.mode === "until_reset" ? (
        <p className="text-xs text-foreground/60">
          {messages.routingStrategyDialog.previewBanProjection(data.ban_projection.mode, data.ban_projection.cumulative_retry_attempt_threshold)}
        </p>
      ) : null}
    </div>
  )
}

function PreviewDetails({ copy, label }: { copy: ReturnType<typeof useLocale>["messages"]["routingStrategyDialog"]; label: string }) {
  return (
    <details className="text-xs text-foreground/60">
      <summary className="cursor-pointer font-medium text-foreground">{label}</summary>
      <p className="pt-2">{copy.previewDescriptionDetails}</p>
    </details>
  )
}

function terminationCopy(reason: StrategyPreviewResponse["termination_reason"], attempt: number, copy: ReturnType<typeof useLocale>["messages"]["routingStrategyDialog"]): string {
  switch (reason) {
    case "cycle_exhausted":
      return copy.previewTerminationCycleExhausted.replace("{attempt}", String(attempt))
    case "ban_transition":
      return copy.previewTerminationBanTransition.replace("{attempt}", String(attempt))
    case "five_step_limit":
      return copy.previewTerminationFiveStepLimit
  }
}
