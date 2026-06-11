import { zodResolver } from "@hookform/resolvers/zod"
import { useEffect } from "react"
import { useForm, type Resolver, type UseFormRegisterReturn } from "react-hook-form"
import type { LoadbalanceStrategy } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldSet, FieldLegend } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/i18n/useLocale"
import { getLegacyLoadbalanceStrategyLabel, getLegacyLoadbalanceStrategySummary } from "@/lib/loadbalanceRoutingPolicy"
import { banPolicyFormSchema, banPolicyModes, banPolicyRoutingTypes, DEFAULT_BAN_POLICY_FORM_VALUES, type BanPolicyFormValues } from "./banPolicySchemas"

interface BanPolicyDialogProps {
  editingStrategy: LoadbalanceStrategy | null
  initialValues: BanPolicyFormValues
  open: boolean
  saving: boolean
  onClose: () => void
  onOpenChange: (open: boolean) => void
  onSave: (values: BanPolicyFormValues) => Promise<void>
}

export function BanPolicyDialog({ editingStrategy, initialValues, open, saving, onClose, onOpenChange, onSave }: BanPolicyDialogProps) {
  const { messages } = useLocale()
  const copy = messages.loadbalanceStrategyDialog
  const strategyCopy = messages.loadbalanceStrategyCopy
  const form = useForm<BanPolicyFormValues>({
    resolver: zodResolver(banPolicyFormSchema) as unknown as Resolver<BanPolicyFormValues>,
    defaultValues: DEFAULT_BAN_POLICY_FORM_VALUES,
    mode: "onSubmit",
  })
  const banMode = form.watch("ban_mode")
  const strategyType = form.watch("legacy_strategy_type")

  useEffect(() => {
    if (open) form.reset(initialValues)
  }, [form, initialValues, open])

  const updateBanMode = (value: BanPolicyFormValues["ban_mode"]) => {
    const current = form.getValues()
    form.setValue("ban_mode", value, { shouldDirty: true, shouldValidate: true })
    if (value === "off") {
      form.setValue("ban_cumulative_retry_attempt_threshold", 0, { shouldDirty: true, shouldValidate: true })
      form.setValue("ban_duration_seconds", 0, { shouldDirty: true, shouldValidate: true })
    } else if (current.ban_mode === "off" && current.ban_cumulative_retry_attempt_threshold === 0) {
      form.setValue("ban_cumulative_retry_attempt_threshold", current.cycle_retry_attempt_limit * 2, { shouldDirty: true, shouldValidate: true })
    }
    if (value === "temporary" && current.ban_duration_seconds < 1) {
      form.setValue("ban_duration_seconds", 1, { shouldDirty: true, shouldValidate: true })
    }
    if (value === "until_reset") {
      form.setValue("ban_duration_seconds", 0, { shouldDirty: true, shouldValidate: true })
    }
  }

  const updateCycleLimit = (rawValue: string) => {
    const nextLimit = Number(rawValue)
    form.setValue("cycle_retry_attempt_limit", Number.isFinite(nextLimit) ? Math.trunc(nextLimit) : 1, { shouldDirty: true, shouldValidate: true })
    const threshold = form.getValues("ban_cumulative_retry_attempt_threshold")
    if (threshold !== 0 && nextLimit > threshold) {
      form.setValue("ban_cumulative_retry_attempt_threshold", Math.trunc(nextLimit), { shouldDirty: true, shouldValidate: true })
    }
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (nextOpen) onOpenChange(true); else onClose() }}>
      <DialogContent className="flex h-[min(94vh,56rem)] max-h-[94vh] max-w-3xl flex-col overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b bg-background px-6 py-5 sm:px-7">
          <DialogTitle>{editingStrategy ? "Edit Ban Policy Strategy" : "Create Ban Policy Strategy"}</DialogTitle>
          <DialogDescription>{copy.description}</DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => onSave(values))} className="flex min-h-0 flex-1 flex-col">
          <DialogBody className="min-h-0 flex-1 overflow-y-auto px-6 py-5 sm:px-7">
            <FieldGroup className="gap-6">
              <FieldSet className="rounded-2xl border bg-muted/20 p-4 sm:p-5">
                <FieldLegend>{copy.basicsSectionTitle}</FieldLegend>
                <Field data-invalid={Boolean(form.formState.errors.name)}>
                  <FieldLabel htmlFor="ban-policy-name">{copy.nameLabel}</FieldLabel>
                  <Input id="ban-policy-name" autoComplete="off" placeholder={copy.namePlaceholder} aria-invalid={Boolean(form.formState.errors.name)} {...form.register("name")} />
                  <FieldError>{form.formState.errors.name?.message}</FieldError>
                </Field>
              </FieldSet>

              <FieldSet className="rounded-2xl border bg-muted/20 p-4 sm:p-5">
                <FieldLegend>{copy.strategyBehaviorSectionTitle}</FieldLegend>
                <Field>
                  <FieldLabel htmlFor="ban-policy-routing">Routing family</FieldLabel>
                  <Select value={strategyType} onValueChange={(value) => form.setValue("legacy_strategy_type", value as BanPolicyFormValues["legacy_strategy_type"], { shouldDirty: true, shouldValidate: true })}>
                    <SelectTrigger id="ban-policy-routing"><SelectValue /></SelectTrigger>
                    <SelectContent>{banPolicyRoutingTypes.map((type) => <SelectItem key={type} value={type}>{getLegacyLoadbalanceStrategyLabel(type, strategyCopy)}</SelectItem>)}</SelectContent>
                  </Select>
                  <FieldDescription>{getLegacyLoadbalanceStrategySummary(strategyType, strategyCopy)}</FieldDescription>
                </Field>
              </FieldSet>

              <FieldSet className="rounded-2xl border bg-muted/20 p-4 sm:p-5">
                <FieldLegend>{copy.reliabilityControlsSectionTitle}</FieldLegend>
                <div className="grid gap-4 md:grid-cols-2">
                  <NumberField id="retry-base" label={copy.retryBaseDelayLabel} description={copy.retryBaseDelayDescription} error={form.formState.errors.retry_base_delay_ms?.message} inputProps={form.register("retry_base_delay_ms", { valueAsNumber: true })} min={0} max={86400000} />
                  <NumberField id="retry-max" label={copy.retryMaxDelayLabel} description={copy.retryMaxDelayDescription} error={form.formState.errors.retry_max_delay_ms?.message} inputProps={form.register("retry_max_delay_ms", { valueAsNumber: true })} min={1} max={86400000} />
                  <NumberField id="retry-backoff" label={copy.backoffMultiplierLabel} description={copy.backoffMultiplierDescription} error={form.formState.errors.retry_backoff_multiplier?.message} inputProps={form.register("retry_backoff_multiplier", { valueAsNumber: true })} min={1} max={10} step={0.1} />
                  <NumberField id="retry-jitter" label={copy.retryJitterRatioLabel} description={copy.retryJitterRatioDescription} error={form.formState.errors.retry_jitter_ratio?.message} inputProps={form.register("retry_jitter_ratio", { valueAsNumber: true })} min={0} max={1} step={0.01} />
                  <Field data-invalid={Boolean(form.formState.errors.cycle_retry_attempt_limit)}>
                    <FieldLabel htmlFor="cycle-limit">{copy.cycleRetryAttemptLimitLabel}</FieldLabel>
                    <FieldDescription>{copy.cycleRetryAttemptLimitDescription}</FieldDescription>
                    <Input id="cycle-limit" type="number" min={1} max={50} step={1} aria-invalid={Boolean(form.formState.errors.cycle_retry_attempt_limit)} value={form.watch("cycle_retry_attempt_limit")} onChange={(event) => updateCycleLimit(event.target.value)} />
                    <FieldError>{form.formState.errors.cycle_retry_attempt_limit?.message}</FieldError>
                  </Field>
                  <NumberField id="cumulative-threshold" label={copy.banCumulativeRetryAttemptThresholdLabel} description={copy.banCumulativeRetryAttemptThresholdDescription} error={form.formState.errors.ban_cumulative_retry_attempt_threshold?.message} inputProps={form.register("ban_cumulative_retry_attempt_threshold", { valueAsNumber: true })} min={0} max={500} />
                </div>
                <Field data-invalid={Boolean(form.formState.errors.failure_status_codes_input)}>
                  <FieldLabel htmlFor="failure-status-codes">{copy.failureStatusCodesLabel}</FieldLabel>
                  <FieldDescription>{copy.failureStatusCodesDescription}</FieldDescription>
                  <Textarea id="failure-status-codes" rows={3} placeholder="403, 422, 429, 500, 502, 503, 504, 529" aria-invalid={Boolean(form.formState.errors.failure_status_codes_input)} {...form.register("failure_status_codes_input")} />
                  <FieldError>{form.formState.errors.failure_status_codes_input?.message}</FieldError>
                </Field>
                <div className="grid gap-4 md:grid-cols-2">
                  <Field data-invalid={Boolean(form.formState.errors.ban_mode)}>
                    <FieldLabel htmlFor="ban-mode">{copy.banModeLabel}</FieldLabel>
                    <FieldDescription>{copy.banModeDescription}</FieldDescription>
                    <Select value={banMode} onValueChange={(value) => updateBanMode(value as BanPolicyFormValues["ban_mode"])}>
                      <SelectTrigger id="ban-mode"><SelectValue /></SelectTrigger>
                      <SelectContent>{banPolicyModes.map((mode) => <SelectItem key={mode} value={mode}>{mode === "off" ? copy.banModeOffOption : mode === "temporary" ? copy.banModeTemporaryOption : copy.banModeUntilResetOption}</SelectItem>)}</SelectContent>
                    </Select>
                    <FieldError>{form.formState.errors.ban_mode?.message}</FieldError>
                  </Field>
                  {banMode === "temporary" ? <NumberField id="ban-duration" label={copy.banDurationLabel} description={copy.banDurationDescription} error={form.formState.errors.ban_duration_seconds?.message} inputProps={form.register("ban_duration_seconds", { valueAsNumber: true })} min={1} max={86400} /> : null}
                </div>
              </FieldSet>
            </FieldGroup>
          </DialogBody>
          <DialogFooter className="shrink-0 border-t bg-background px-6 py-4 sm:px-7">
            <Button type="button" variant="outline" onClick={onClose}>{copy.cancel}</Button>
            <Button type="submit" disabled={saving}>{saving ? copy.saving : copy.save}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NumberField({ id, label, description, error, inputProps, min, max, step = 1 }: { id: string; label: string; description: string; error?: string; inputProps: UseFormRegisterReturn; min: number; max: number; step?: number }) {
  return <Field data-invalid={Boolean(error)}><FieldLabel htmlFor={id}>{label}</FieldLabel><FieldDescription>{description}</FieldDescription><Input id={id} type="number" min={min} max={max} step={step} aria-invalid={Boolean(error)} {...inputProps} /><FieldError>{error}</FieldError></Field>
}
