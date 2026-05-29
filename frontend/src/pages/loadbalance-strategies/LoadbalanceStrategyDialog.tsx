import type { Dispatch, FormEvent, ReactNode, SetStateAction } from "react";
import { Plus, X } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
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
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  getLegacyLoadbalanceStrategySummary,
  LOADBALANCE_BAN_MODES,
  LOADBALANCE_LEGACY_STRATEGY_TYPES,
} from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceBanMode, LoadbalanceStrategy } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  addCircuitBreakerStatusCode,
  getCircuitBreakerStatusCodeInputError,
  removeCircuitBreakerStatusCode,
  setLegacyLoadbalanceStrategyType,
  setLoadbalanceStrategyBanMode,
  setLoadbalanceStrategyCycleRetryAttemptLimit,
  type LoadbalanceStrategyFormState,
} from "./loadbalanceStrategyFormState";

interface LoadbalanceStrategyDialogProps {
  editingLoadbalanceStrategy: LoadbalanceStrategy | null;
  loadbalanceStrategyForm: LoadbalanceStrategyFormState;
  loadbalanceStrategySaving: boolean;
  onClose: () => void;
  onOpenChange: (open: boolean) => void;
  onSave: () => Promise<void>;
  open: boolean;
  setLoadbalanceStrategyForm: Dispatch<SetStateAction<LoadbalanceStrategyFormState>>;
}

interface StrategyDialogSectionProps {
  children: ReactNode;
  className?: string;
  title: string;
}

function StrategyDialogSection({ children, className, title }: StrategyDialogSectionProps) {
  return (
    <section className={cn("flex flex-col gap-4 rounded-2xl border bg-muted/20 p-4 sm:p-5", className)}>
      <div className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold tracking-tight text-foreground">{title}</h2>
      </div>
      {children}
    </section>
  );
}

interface StrategyDialogSubsectionProps {
  children: ReactNode;
  className?: string;
}

function StrategyDialogSubsection({ children, className }: StrategyDialogSubsectionProps) {
  return (
    <div className={cn("flex flex-col gap-3 rounded-xl border bg-background/80 p-4", className)}>
      {children}
    </div>
  );
}

interface StrategyDialogFieldProps {
  children: ReactNode;
  className?: string;
  description?: string;
  id: string;
  label: string;
}

function StrategyDialogField({
  children,
  className,
  description,
  id,
  label,
}: StrategyDialogFieldProps) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <Label htmlFor={id}>{label}</Label>
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      {children}
    </div>
  );
}

export function LoadbalanceStrategyDialog({
  editingLoadbalanceStrategy,
  loadbalanceStrategyForm,
  loadbalanceStrategySaving,
  onClose,
  onOpenChange,
  onSave,
  open,
  setLoadbalanceStrategyForm,
}: LoadbalanceStrategyDialogProps) {
  const { messages } = useLocale();
  const dialogMessages = messages.loadbalanceStrategyDialog;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const statusCodeInputError = getCircuitBreakerStatusCodeInputError(loadbalanceStrategyForm);
  const legacyStrategySummary = getLegacyLoadbalanceStrategySummary(
    loadbalanceStrategyForm.legacy_strategy_type,
    strategyCopy,
  );

  const parseNumericInput = (value: string, fallback: number) => {
    if (!value.trim()) {
      return fallback;
    }
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  };

  const parseIntegerInput = (value: string, fallback: number) => {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? Math.trunc(parsed) : fallback;
  };

  const getBanModeOptionLabel = (banMode: LoadbalanceBanMode) =>
    banMode === "off"
      ? dialogMessages.banModeOffOption
      : banMode === "until_reset"
        ? dialogMessages.banModeUntilResetOption
        : dialogMessages.banModeTemporaryOption;

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void onSave();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="flex h-[min(94vh,56rem)] max-h-[94vh] max-w-3xl flex-col overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b bg-background px-6 py-5 sm:px-7">
          <DialogTitle>
            {editingLoadbalanceStrategy ? dialogMessages.editTitle : dialogMessages.addTitle}
          </DialogTitle>
          <DialogDescription>{dialogMessages.description}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
          <DialogBody className="min-h-0 flex-1 p-0">
            <ScrollArea className="min-h-0 flex-1">
              <div className="flex flex-col gap-6 px-6 py-5 sm:px-7" data-testid="loadbalance-strategy-scroll-body">
                <StrategyDialogSection title={dialogMessages.basicsSectionTitle}>
                  <StrategyDialogField id="loadbalance-strategy-name" label={dialogMessages.nameLabel}>
                    <Input
                      id="loadbalance-strategy-name"
                      name="name"
                      autoComplete="off"
                      value={loadbalanceStrategyForm.name}
                      onChange={(event) =>
                        setLoadbalanceStrategyForm((prev) => ({ ...prev, name: event.target.value }))
                      }
                      placeholder={dialogMessages.namePlaceholder}
                    />
                  </StrategyDialogField>
                </StrategyDialogSection>

                <StrategyDialogSection title={dialogMessages.strategyBehaviorSectionTitle}>
                  <StrategyDialogSubsection>
                    <StrategyDialogField
                      id="loadbalance-strategy-type"
                      label={dialogMessages.legacyStrategyTypeLabel}
                    >
                      <Select
                        value={loadbalanceStrategyForm.legacy_strategy_type}
                        onValueChange={(value) =>
                          setLoadbalanceStrategyForm((prev) =>
                            setLegacyLoadbalanceStrategyType(
                              prev,
                              value as (typeof LOADBALANCE_LEGACY_STRATEGY_TYPES)[number],
                            ),
                          )
                        }
                      >
                        <SelectTrigger id="loadbalance-strategy-type" className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="single">{strategyCopy.singleLabel}</SelectItem>
                          <SelectItem value="fill-first">{strategyCopy.fillFirstLabel}</SelectItem>
                          <SelectItem value="round-robin">{strategyCopy.roundRobinLabel}</SelectItem>
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">{legacyStrategySummary}</p>
                    </StrategyDialogField>
                  </StrategyDialogSubsection>
                </StrategyDialogSection>

                <StrategyDialogSection title={dialogMessages.reliabilityControlsSectionTitle}>
                  <StrategyDialogSubsection>
                    <div className="grid gap-4 md:grid-cols-2">
                      <StrategyDialogField
                        id="ban-policy-retry-base-delay-ms"
                        label={dialogMessages.retryBaseDelayLabel}
                        description={dialogMessages.retryBaseDelayDescription}
                      >
                        <Input
                          id="ban-policy-retry-base-delay-ms"
                          type="number"
                          autoComplete="off"
                          min={0}
                          max={86_400_000}
                          step={1}
                          value={loadbalanceStrategyForm.retry_base_delay_ms}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              retry_base_delay_ms: parseIntegerInput(
                                event.target.value,
                                prev.retry_base_delay_ms,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>

                      <StrategyDialogField
                        id="ban-policy-retry-backoff-multiplier"
                        label={dialogMessages.backoffMultiplierLabel}
                        description={dialogMessages.backoffMultiplierDescription}
                      >
                        <Input
                          id="ban-policy-retry-backoff-multiplier"
                          type="number"
                          autoComplete="off"
                          min={1}
                          max={10}
                          step={0.1}
                          value={loadbalanceStrategyForm.retry_backoff_multiplier}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              retry_backoff_multiplier: parseNumericInput(
                                event.target.value,
                                prev.retry_backoff_multiplier,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>

                      <StrategyDialogField
                        id="ban-policy-retry-jitter-ratio"
                        label={dialogMessages.retryJitterRatioLabel}
                        description={dialogMessages.retryJitterRatioDescription}
                      >
                        <Input
                          id="ban-policy-retry-jitter-ratio"
                          type="number"
                          autoComplete="off"
                          min={0}
                          max={1}
                          step={0.01}
                          value={loadbalanceStrategyForm.retry_jitter_ratio}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              retry_jitter_ratio: parseNumericInput(
                                event.target.value,
                                prev.retry_jitter_ratio,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>

                      <StrategyDialogField
                        id="ban-policy-retry-max-delay-ms"
                        label={dialogMessages.retryMaxDelayLabel}
                        description={dialogMessages.retryMaxDelayDescription}
                      >
                        <Input
                          id="ban-policy-retry-max-delay-ms"
                          type="number"
                          autoComplete="off"
                          min={1}
                          max={86_400_000}
                          step={1}
                          value={loadbalanceStrategyForm.retry_max_delay_ms}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              retry_max_delay_ms: parseIntegerInput(
                                event.target.value,
                                prev.retry_max_delay_ms,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>

                      <StrategyDialogField
                        id="ban-policy-cycle-retry-attempt-limit"
                        label={dialogMessages.cycleRetryAttemptLimitLabel}
                        description={dialogMessages.cycleRetryAttemptLimitDescription}
                      >
                        <Input
                          id="ban-policy-cycle-retry-attempt-limit"
                          type="number"
                          autoComplete="off"
                          min={1}
                          max={50}
                          step={1}
                          value={loadbalanceStrategyForm.cycle_retry_attempt_limit}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) =>
                              setLoadbalanceStrategyCycleRetryAttemptLimit(
                                prev,
                                parseIntegerInput(event.target.value, prev.cycle_retry_attempt_limit),
                              ),
                            )
                          }
                        />
                      </StrategyDialogField>

                      <StrategyDialogField
                        id="ban-policy-cumulative-threshold"
                        label={dialogMessages.banCumulativeRetryAttemptThresholdLabel}
                        description={dialogMessages.banCumulativeRetryAttemptThresholdDescription}
                      >
                        <Input
                          id="ban-policy-cumulative-threshold"
                          type="number"
                          autoComplete="off"
                          min={0}
                          max={500}
                          step={1}
                          value={loadbalanceStrategyForm.ban_cumulative_retry_attempt_threshold}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              ban_cumulative_retry_attempt_threshold: parseIntegerInput(
                                event.target.value,
                                prev.ban_cumulative_retry_attempt_threshold,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>
                    </div>
                  </StrategyDialogSubsection>

                  <StrategyDialogSubsection>
                    <StrategyDialogField
                      id="ban-policy-status-code-input"
                      label={dialogMessages.failureStatusCodesLabel}
                      description={dialogMessages.failureStatusCodesDescription}
                    >
                      <div className="flex flex-col gap-2 sm:flex-row">
                        <Input
                          id="ban-policy-status-code-input"
                          inputMode="numeric"
                          autoComplete="off"
                          value={loadbalanceStrategyForm.status_code_input}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              status_code_input: event.target.value,
                            }))
                          }
                          placeholder="429"
                        />
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() =>
                            setLoadbalanceStrategyForm((prev) => addCircuitBreakerStatusCode(prev))
                          }
                        >
                          <Plus className="mr-2 h-4 w-4" />
                          {dialogMessages.addStatusCode}
                        </Button>
                      </div>
                      {statusCodeInputError ? (
                        <p className="text-sm text-destructive">{statusCodeInputError}</p>
                      ) : null}
                    </StrategyDialogField>

                    <div className="flex flex-wrap gap-2">
                      {loadbalanceStrategyForm.failure_status_codes.map((statusCode) => (
                        <button
                          key={statusCode}
                          type="button"
                          className="inline-flex items-center gap-1 rounded-full border bg-background px-2.5 py-1 text-xs font-medium text-foreground"
                          onClick={() =>
                            setLoadbalanceStrategyForm((prev) =>
                              removeCircuitBreakerStatusCode(prev, statusCode),
                            )
                          }
                          aria-label={dialogMessages.removeStatusCode(statusCode)}
                        >
                          <span>{statusCode}</span>
                          <X className="h-3 w-3" />
                        </button>
                      ))}
                    </div>
                  </StrategyDialogSubsection>

                  <StrategyDialogSubsection>
                    <StrategyDialogField
                      id="ban-policy-ban-mode"
                      label={dialogMessages.banModeLabel}
                      description={dialogMessages.banModeDescription}
                    >
                      <Select
                        value={loadbalanceStrategyForm.ban_mode}
                        onValueChange={(value) =>
                          setLoadbalanceStrategyForm((prev) =>
                            setLoadbalanceStrategyBanMode(prev, value as LoadbalanceBanMode),
                          )
                        }
                      >
                        <SelectTrigger id="ban-policy-ban-mode" className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {LOADBALANCE_BAN_MODES.map((banMode) => (
                            <SelectItem key={banMode} value={banMode}>
                              {getBanModeOptionLabel(banMode)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </StrategyDialogField>

                    {loadbalanceStrategyForm.ban_mode === "temporary" ? (
                      <StrategyDialogField
                        id="ban-policy-ban-duration-seconds"
                        label={dialogMessages.banDurationLabel}
                        description={dialogMessages.banDurationDescription}
                      >
                        <Input
                          id="ban-policy-ban-duration-seconds"
                          type="number"
                          autoComplete="off"
                          min={1}
                          max={86_400}
                          step={1}
                          value={loadbalanceStrategyForm.ban_duration_seconds}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) => ({
                              ...prev,
                              ban_duration_seconds: parseIntegerInput(
                                event.target.value,
                                prev.ban_duration_seconds,
                              ),
                            }))
                          }
                        />
                      </StrategyDialogField>
                    ) : null}
                  </StrategyDialogSubsection>
                </StrategyDialogSection>
              </div>
            </ScrollArea>
          </DialogBody>

          <DialogFooter className="shrink-0 border-t bg-background px-6 py-4 sm:px-7">
            <Button type="button" variant="outline" onClick={onClose}>
              {dialogMessages.cancel}
            </Button>
            <Button type="submit" disabled={loadbalanceStrategySaving}>
              {loadbalanceStrategySaving ? dialogMessages.saving : dialogMessages.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
