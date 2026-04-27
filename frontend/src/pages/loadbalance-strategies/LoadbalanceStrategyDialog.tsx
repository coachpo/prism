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
  getAdaptiveRoutingObjectiveLabel,
  LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES,
  LOADBALANCE_LEGACY_STRATEGY_TYPES,
} from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  addCircuitBreakerStatusCode,
  getCircuitBreakerStatusCodeInputError,
  removeCircuitBreakerStatusCode,
  setLegacyLoadbalanceStrategyType,
  setLoadbalanceStrategyAutoRecoveryMode,
  setLoadbalanceStrategyBanMode,
  setLoadbalanceStrategyFamily,
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
  const legacyForm =
    loadbalanceStrategyForm.strategy_type === "legacy" ? loadbalanceStrategyForm : null;
  const adaptiveForm =
    loadbalanceStrategyForm.strategy_type === "adaptive" ? loadbalanceStrategyForm : null;
  const enabledAutoRecovery =
    legacyForm?.auto_recovery.mode === "enabled" ? legacyForm.auto_recovery : null;
  const adaptiveCircuitBreaker = adaptiveForm?.routing_policy.circuit_breaker ?? null;
  const circuitBreakerStatusCodeInputState = enabledAutoRecovery
    ? enabledAutoRecovery
    : adaptiveForm
      ? {
          status_codes: adaptiveForm.routing_policy.circuit_breaker.failure_status_codes,
          status_code_input: adaptiveForm.circuit_breaker_status_code_input,
        }
      : null;
  const statusCodeInputError = circuitBreakerStatusCodeInputState
    ? getCircuitBreakerStatusCodeInputError(circuitBreakerStatusCodeInputState)
    : null;
  const legacyStrategySummary = legacyForm
    ? {
        single: strategyCopy.singleSummary,
        "fill-first": strategyCopy.fillFirstSummary,
        "round-robin": strategyCopy.roundRobinSummary,
      }[legacyForm.legacy_strategy_type]
    : null;
  const adaptiveRoutingSummary = adaptiveForm
    ? {
        minimize_latency: strategyCopy.minimizeLatencySummary,
        maximize_availability: strategyCopy.maximizeAvailabilitySummary,
      }[adaptiveForm.routing_policy.routing_objective]
    : null;
  const strategyBehaviorLabel = legacyForm
    ? dialogMessages.legacyStrategyTypeLabel
    : dialogMessages.routingPolicyLabel;
  const strategyBehaviorSummary = legacyForm ? legacyStrategySummary : adaptiveRoutingSummary;
  const hasReliabilityConfiguration = Boolean(enabledAutoRecovery || adaptiveCircuitBreaker);
  const visibleStatusCodes = enabledAutoRecovery?.status_codes ?? adaptiveCircuitBreaker?.failure_status_codes ?? [];

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
                <div className="grid gap-4 md:grid-cols-2">
                  <StrategyDialogField
                    id="loadbalance-strategy-name"
                    label={dialogMessages.nameLabel}
                  >
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

                  <StrategyDialogField
                    id="loadbalance-strategy-family"
                    label={dialogMessages.strategyFamilyLabel}
                  >
                    <Select
                      value={loadbalanceStrategyForm.strategy_type}
                      onValueChange={(value) =>
                        setLoadbalanceStrategyForm((prev) =>
                          setLoadbalanceStrategyFamily(prev, value as "legacy" | "adaptive"),
                        )
                      }
                      disabled={Boolean(editingLoadbalanceStrategy)}
                    >
                      <SelectTrigger id="loadbalance-strategy-family" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="legacy">{strategyCopy.legacyFamilyLabel}</SelectItem>
                        <SelectItem value="adaptive">{strategyCopy.adaptiveFamilyLabel}</SelectItem>
                      </SelectContent>
                    </Select>
                  </StrategyDialogField>
                </div>
              </StrategyDialogSection>

              <StrategyDialogSection title={dialogMessages.strategyBehaviorSectionTitle}>
                <StrategyDialogSubsection key={loadbalanceStrategyForm.strategy_type}>
                  {legacyForm ? (
                    <StrategyDialogField
                      id="loadbalance-strategy-type"
                      label={strategyBehaviorLabel}
                    >
                      <Select
                        value={legacyForm.legacy_strategy_type}
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
                      {strategyBehaviorSummary ? (
                        <p className="text-xs text-muted-foreground">{strategyBehaviorSummary}</p>
                      ) : null}
                    </StrategyDialogField>
                  ) : adaptiveForm ? (
                    <StrategyDialogField id="adaptive-routing-policy" label={strategyBehaviorLabel}>
                      <Select
                        value={adaptiveForm.routing_policy.routing_objective}
                        onValueChange={(value) =>
                          setLoadbalanceStrategyForm((prev) =>
                            prev.strategy_type !== "adaptive"
                              ? prev
                              : {
                                  ...prev,
                                  routing_policy: {
                                    ...prev.routing_policy,
                                    routing_objective:
                                      value as (typeof LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES)[number],
                                  },
                                },
                          )
                        }
                      >
                        <SelectTrigger id="adaptive-routing-policy" className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES.map((routingObjective) => (
                            <SelectItem key={routingObjective} value={routingObjective}>
                              {getAdaptiveRoutingObjectiveLabel(routingObjective, strategyCopy)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      {strategyBehaviorSummary ? (
                        <p className="text-xs text-muted-foreground">{strategyBehaviorSummary}</p>
                      ) : null}
                    </StrategyDialogField>
                  ) : null}
                </StrategyDialogSubsection>
              </StrategyDialogSection>

              <StrategyDialogSection title={dialogMessages.reliabilityControlsSectionTitle}>
                {legacyForm ? (
                  <StrategyDialogSubsection>
                    <StrategyDialogField
                      id="loadbalance-strategy-auto-recovery-mode"
                      label={dialogMessages.autoRecoveryLabel}
                    >
                      <Select
                        value={legacyForm.auto_recovery.mode}
                        onValueChange={(value) =>
                          setLoadbalanceStrategyForm((prev) =>
                            setLoadbalanceStrategyAutoRecoveryMode(
                              prev,
                              value as "disabled" | "enabled",
                            ),
                          )
                        }
                      >
                        <SelectTrigger
                          id="loadbalance-strategy-auto-recovery-mode"
                          className="w-full"
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="disabled">
                            {dialogMessages.autoRecoveryDisabledOption}
                          </SelectItem>
                          <SelectItem value="enabled">
                            {dialogMessages.autoRecoveryEnabledOption}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    </StrategyDialogField>
                  </StrategyDialogSubsection>
                ) : null}

                {hasReliabilityConfiguration ? (
                  <>
                    <StrategyDialogSubsection>
                      {enabledAutoRecovery ? (
                        <div className="grid gap-4 md:grid-cols-2">
                          <StrategyDialogField
                            id="circuit-breaker-base-open-seconds"
                            label={dialogMessages.baseCooldownLabel}
                            description={dialogMessages.baseCooldownDescription}
                          >
                            <Input
                              id="circuit-breaker-base-open-seconds"
                              type="number"
                              autoComplete="off"
                              min={0}
                              step={1}
                              value={enabledAutoRecovery.cooldown.base_seconds}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "legacy" ||
                                  prev.auto_recovery.mode !== "enabled"
                                    ? prev
                                    : {
                                        ...prev,
                                        auto_recovery: {
                                          ...prev.auto_recovery,
                                          cooldown: {
                                            ...prev.auto_recovery.cooldown,
                                            base_seconds: parseIntegerInput(
                                              event.target.value,
                                              prev.auto_recovery.cooldown.base_seconds,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="circuit-breaker-failure-threshold"
                            label={dialogMessages.failureThresholdLabel}
                            description={dialogMessages.failureThresholdDescription}
                          >
                            <Input
                              id="circuit-breaker-failure-threshold"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={50}
                              step={1}
                              value={enabledAutoRecovery.cooldown.failure_threshold}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "legacy" ||
                                  prev.auto_recovery.mode !== "enabled"
                                    ? prev
                                    : {
                                        ...prev,
                                        auto_recovery: {
                                          ...prev.auto_recovery,
                                          cooldown: {
                                            ...prev.auto_recovery.cooldown,
                                            failure_threshold: parseIntegerInput(
                                              event.target.value,
                                              prev.auto_recovery.cooldown.failure_threshold,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="circuit-breaker-backoff-multiplier"
                            label={dialogMessages.backoffMultiplierLabel}
                            description={dialogMessages.backoffMultiplierDescription}
                          >
                            <Input
                              id="circuit-breaker-backoff-multiplier"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={10}
                              step={0.1}
                              value={enabledAutoRecovery.cooldown.backoff_multiplier}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "legacy" ||
                                  prev.auto_recovery.mode !== "enabled"
                                    ? prev
                                    : {
                                        ...prev,
                                        auto_recovery: {
                                          ...prev.auto_recovery,
                                          cooldown: {
                                            ...prev.auto_recovery.cooldown,
                                            backoff_multiplier: parseNumericInput(
                                              event.target.value,
                                              prev.auto_recovery.cooldown.backoff_multiplier,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="circuit-breaker-max-open-seconds"
                            label={dialogMessages.maxCooldownLabel}
                            description={dialogMessages.maxCooldownDescription}
                          >
                            <Input
                              id="circuit-breaker-max-open-seconds"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={86400}
                              step={1}
                              value={enabledAutoRecovery.cooldown.max_cooldown_seconds}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "legacy" ||
                                  prev.auto_recovery.mode !== "enabled"
                                    ? prev
                                    : {
                                        ...prev,
                                        auto_recovery: {
                                          ...prev.auto_recovery,
                                          cooldown: {
                                            ...prev.auto_recovery.cooldown,
                                            max_cooldown_seconds: parseIntegerInput(
                                              event.target.value,
                                              prev.auto_recovery.cooldown.max_cooldown_seconds,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                        </div>
                      ) : adaptiveCircuitBreaker ? (
                        <div className="grid gap-4 md:grid-cols-2">
                          <StrategyDialogField
                            id="adaptive-circuit-breaker-base-open-seconds"
                            label={dialogMessages.baseCooldownLabel}
                            description={dialogMessages.baseCooldownDescription}
                          >
                            <Input
                              id="adaptive-circuit-breaker-base-open-seconds"
                              type="number"
                              autoComplete="off"
                              min={0}
                              step={1}
                              value={adaptiveCircuitBreaker.base_open_seconds}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "adaptive"
                                    ? prev
                                    : {
                                        ...prev,
                                        routing_policy: {
                                          ...prev.routing_policy,
                                          circuit_breaker: {
                                            ...prev.routing_policy.circuit_breaker,
                                            base_open_seconds: parseIntegerInput(
                                              event.target.value,
                                              prev.routing_policy.circuit_breaker.base_open_seconds,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="adaptive-circuit-breaker-failure-threshold"
                            label={dialogMessages.failureThresholdLabel}
                            description={dialogMessages.failureThresholdDescription}
                          >
                            <Input
                              id="adaptive-circuit-breaker-failure-threshold"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={50}
                              step={1}
                              value={adaptiveCircuitBreaker.failure_threshold}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "adaptive"
                                    ? prev
                                    : {
                                        ...prev,
                                        routing_policy: {
                                          ...prev.routing_policy,
                                          circuit_breaker: {
                                            ...prev.routing_policy.circuit_breaker,
                                            failure_threshold: parseIntegerInput(
                                              event.target.value,
                                              prev.routing_policy.circuit_breaker.failure_threshold,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="adaptive-circuit-breaker-backoff-multiplier"
                            label={dialogMessages.backoffMultiplierLabel}
                            description={dialogMessages.backoffMultiplierDescription}
                          >
                            <Input
                              id="adaptive-circuit-breaker-backoff-multiplier"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={10}
                              step={0.1}
                              value={adaptiveCircuitBreaker.backoff_multiplier}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "adaptive"
                                    ? prev
                                    : {
                                        ...prev,
                                        routing_policy: {
                                          ...prev.routing_policy,
                                          circuit_breaker: {
                                            ...prev.routing_policy.circuit_breaker,
                                            backoff_multiplier: parseNumericInput(
                                              event.target.value,
                                              prev.routing_policy.circuit_breaker.backoff_multiplier,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                          <StrategyDialogField
                            id="adaptive-circuit-breaker-max-open-seconds"
                            label={dialogMessages.maxCooldownLabel}
                            description={dialogMessages.maxCooldownDescription}
                          >
                            <Input
                              id="adaptive-circuit-breaker-max-open-seconds"
                              type="number"
                              autoComplete="off"
                              min={1}
                              max={86400}
                              step={1}
                              value={adaptiveCircuitBreaker.max_open_seconds}
                              onChange={(event) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  prev.strategy_type !== "adaptive"
                                    ? prev
                                    : {
                                        ...prev,
                                        routing_policy: {
                                          ...prev.routing_policy,
                                          circuit_breaker: {
                                            ...prev.routing_policy.circuit_breaker,
                                            max_open_seconds: parseIntegerInput(
                                              event.target.value,
                                              prev.routing_policy.circuit_breaker.max_open_seconds,
                                            ),
                                          },
                                        },
                                      },
                                )
                              }
                            />
                          </StrategyDialogField>

                        </div>
                      ) : null}
                    </StrategyDialogSubsection>

                    <StrategyDialogSubsection>
                      <StrategyDialogField
                        id={
                          enabledAutoRecovery
                            ? "circuit-breaker-status-code-input"
                            : "adaptive-circuit-breaker-status-code-input"
                        }
                        label={dialogMessages.failureStatusCodesLabel}
                        description={dialogMessages.failureStatusCodesDescription}
                      >
                        <div className="flex flex-col gap-2 sm:flex-row">
                          <Input
                            id={
                              enabledAutoRecovery
                                ? "circuit-breaker-status-code-input"
                                : "adaptive-circuit-breaker-status-code-input"
                            }
                            inputMode="numeric"
                            autoComplete="off"
                            value={
                              enabledAutoRecovery
                                ? enabledAutoRecovery.status_code_input
                                : adaptiveForm?.circuit_breaker_status_code_input ?? ""
                            }
                            onChange={(event) =>
                              setLoadbalanceStrategyForm((prev) =>
                                prev.strategy_type === "legacy" &&
                                prev.auto_recovery.mode === "enabled"
                                  ? {
                                      ...prev,
                                      auto_recovery: {
                                        ...prev.auto_recovery,
                                        status_code_input: event.target.value,
                                      },
                                    }
                                  : prev.strategy_type === "adaptive"
                                    ? {
                                        ...prev,
                                        circuit_breaker_status_code_input: event.target.value,
                                      }
                                    : prev,
                              )
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
                        {visibleStatusCodes.map((statusCode) => (
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
                      {enabledAutoRecovery ? (
                        <>
                          <StrategyDialogField
                            id="circuit-breaker-ban-mode"
                            label={dialogMessages.banModeLabel}
                            description={dialogMessages.banModeDescription}
                          >
                            <Select
                              value={enabledAutoRecovery.ban.mode}
                              onValueChange={(value) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  setLoadbalanceStrategyBanMode(
                                    prev,
                                    value as "off" | "manual" | "temporary",
                                  ),
                                )
                              }
                            >
                              <SelectTrigger id="circuit-breaker-ban-mode" className="w-full">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="off">{dialogMessages.banModeOffOption}</SelectItem>
                                <SelectItem value="manual">
                                  {dialogMessages.banModeManualOption}
                                </SelectItem>
                                <SelectItem value="temporary">
                                  {dialogMessages.banModeTemporaryOption}
                                </SelectItem>
                              </SelectContent>
                            </Select>
                          </StrategyDialogField>

                          {enabledAutoRecovery.ban.mode !== "off" ? (
                            <div className="grid gap-4 md:grid-cols-2">
                              <StrategyDialogField
                                id="circuit-breaker-max-open-strikes-before-ban"
                                label={dialogMessages.maxCooldownStrikesBeforeBanLabel}
                                description={dialogMessages.maxCooldownStrikesBeforeBanDescription}
                              >
                                <Input
                                  id="circuit-breaker-max-open-strikes-before-ban"
                                  type="number"
                                  autoComplete="off"
                                  min={1}
                                  step={1}
                                  value={enabledAutoRecovery.ban.max_cooldown_strikes_before_ban}
                                  onChange={(event) =>
                                    setLoadbalanceStrategyForm((prev) =>
                                      prev.strategy_type !== "legacy" ||
                                      prev.auto_recovery.mode !== "enabled" ||
                                      prev.auto_recovery.ban.mode === "off"
                                        ? prev
                                        : {
                                            ...prev,
                                            auto_recovery: {
                                              ...prev.auto_recovery,
                                              ban: {
                                                ...prev.auto_recovery.ban,
                                                max_cooldown_strikes_before_ban: parseIntegerInput(
                                                  event.target.value,
                                                  prev.auto_recovery.ban.max_cooldown_strikes_before_ban,
                                                ),
                                              },
                                            },
                                          },
                                    )
                                  }
                                />
                              </StrategyDialogField>

                              {enabledAutoRecovery.ban.mode === "temporary" ? (
                                <StrategyDialogField
                                  id="circuit-breaker-ban-duration-seconds"
                                  label={dialogMessages.banDurationLabel}
                                  description={dialogMessages.banDurationDescription}
                                >
                                  <Input
                                    id="circuit-breaker-ban-duration-seconds"
                                    type="number"
                                    autoComplete="off"
                                    min={1}
                                    step={1}
                                    value={enabledAutoRecovery.ban.ban_duration_seconds}
                                    onChange={(event) =>
                                      setLoadbalanceStrategyForm((prev) =>
                                        prev.strategy_type !== "legacy" ||
                                        prev.auto_recovery.mode !== "enabled" ||
                                        prev.auto_recovery.ban.mode !== "temporary"
                                          ? prev
                                          : {
                                              ...prev,
                                              auto_recovery: {
                                                ...prev.auto_recovery,
                                                ban: {
                                                  ...prev.auto_recovery.ban,
                                                  ban_duration_seconds: parseIntegerInput(
                                                    event.target.value,
                                                    prev.auto_recovery.ban.ban_duration_seconds,
                                                  ),
                                                },
                                              },
                                            },
                                      )
                                    }
                                  />
                                </StrategyDialogField>
                              ) : null}
                            </div>
                          ) : null}
                        </>
                      ) : adaptiveCircuitBreaker ? (
                        <>
                          <StrategyDialogField
                            id="adaptive-circuit-breaker-ban-mode"
                            label={dialogMessages.banModeLabel}
                            description={dialogMessages.banModeDescription}
                          >
                            <Select
                              value={adaptiveCircuitBreaker.ban_mode}
                              onValueChange={(value) =>
                                setLoadbalanceStrategyForm((prev) =>
                                  setLoadbalanceStrategyBanMode(
                                    prev,
                                    value as "off" | "manual" | "temporary",
                                  ),
                                )
                              }
                            >
                              <SelectTrigger
                                id="adaptive-circuit-breaker-ban-mode"
                                className="w-full"
                              >
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="off">{dialogMessages.banModeOffOption}</SelectItem>
                                <SelectItem value="manual">
                                  {dialogMessages.banModeManualOption}
                                </SelectItem>
                                <SelectItem value="temporary">
                                  {dialogMessages.banModeTemporaryOption}
                                </SelectItem>
                              </SelectContent>
                            </Select>
                          </StrategyDialogField>

                          {adaptiveCircuitBreaker.ban_mode !== "off" ? (
                            <div className="grid gap-4 md:grid-cols-2">
                              <StrategyDialogField
                                id="adaptive-circuit-breaker-max-open-strikes-before-ban"
                                label={dialogMessages.maxCooldownStrikesBeforeBanLabel}
                                description={dialogMessages.maxCooldownStrikesBeforeBanDescription}
                              >
                                <Input
                                  id="adaptive-circuit-breaker-max-open-strikes-before-ban"
                                  type="number"
                                  autoComplete="off"
                                  min={1}
                                  step={1}
                                  value={adaptiveCircuitBreaker.max_open_strikes_before_ban}
                                  onChange={(event) =>
                                    setLoadbalanceStrategyForm((prev) =>
                                      prev.strategy_type !== "adaptive" ||
                                      prev.routing_policy.circuit_breaker.ban_mode === "off"
                                        ? prev
                                        : {
                                            ...prev,
                                            routing_policy: {
                                              ...prev.routing_policy,
                                              circuit_breaker: {
                                                ...prev.routing_policy.circuit_breaker,
                                                max_open_strikes_before_ban: parseIntegerInput(
                                                  event.target.value,
                                                  prev.routing_policy.circuit_breaker.max_open_strikes_before_ban,
                                                ),
                                              },
                                            },
                                          },
                                    )
                                  }
                                />
                              </StrategyDialogField>

                              {adaptiveCircuitBreaker.ban_mode === "temporary" ? (
                                <StrategyDialogField
                                  id="adaptive-circuit-breaker-ban-duration-seconds"
                                  label={dialogMessages.banDurationLabel}
                                  description={dialogMessages.banDurationDescription}
                                >
                                  <Input
                                    id="adaptive-circuit-breaker-ban-duration-seconds"
                                    type="number"
                                    autoComplete="off"
                                    min={1}
                                    step={1}
                                    value={adaptiveCircuitBreaker.ban_duration_seconds}
                                    onChange={(event) =>
                                      setLoadbalanceStrategyForm((prev) =>
                                        prev.strategy_type !== "adaptive" ||
                                        prev.routing_policy.circuit_breaker.ban_mode !== "temporary"
                                          ? prev
                                          : {
                                              ...prev,
                                              routing_policy: {
                                                ...prev.routing_policy,
                                                circuit_breaker: {
                                                  ...prev.routing_policy.circuit_breaker,
                                                  ban_duration_seconds: parseIntegerInput(
                                                    event.target.value,
                                                    prev.routing_policy.circuit_breaker.ban_duration_seconds,
                                                  ),
                                                },
                                              },
                                            },
                                      )
                                    }
                                  />
                                </StrategyDialogField>
                              ) : null}
                            </div>
                          ) : null}
                        </>
                      ) : null}
                    </StrategyDialogSubsection>
                  </>
                ) : null}
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
