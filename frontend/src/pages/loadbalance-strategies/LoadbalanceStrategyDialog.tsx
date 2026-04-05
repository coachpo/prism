import type { Dispatch, FormEvent, SetStateAction } from "react";
import { Plus, X } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { LoadbalanceStrategy } from "@/lib/types";
import {
  getAdaptiveRoutingObjectiveLabel,
  LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES,
  LOADBALANCE_LEGACY_STRATEGY_TYPES,
} from "@/lib/loadbalanceRoutingPolicy";
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
  const statusCodeInputError =
    enabledAutoRecovery ? getCircuitBreakerStatusCodeInputError(enabledAutoRecovery) : null;

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
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {editingLoadbalanceStrategy ? dialogMessages.editTitle : dialogMessages.addTitle}
          </DialogTitle>
          <DialogDescription>{dialogMessages.description}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="grid gap-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="loadbalance-strategy-name">{dialogMessages.nameLabel}</Label>
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
          </div>

          <div className="space-y-2">
            <Label htmlFor="loadbalance-strategy-family">{dialogMessages.strategyFamilyLabel}</Label>
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
          </div>

          {legacyForm ? (
            <>
              <div className="space-y-2">
                <Label htmlFor="loadbalance-strategy-type">{dialogMessages.legacyStrategyTypeLabel}</Label>
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
              </div>

              <div className="space-y-2">
                <Label htmlFor="loadbalance-strategy-auto-recovery-mode">
                  {dialogMessages.autoRecoveryLabel}
                </Label>
                <Select
                  value={legacyForm.auto_recovery.mode}
                  onValueChange={(value) =>
                    setLoadbalanceStrategyForm((prev) =>
                      setLoadbalanceStrategyAutoRecoveryMode(prev, value as "disabled" | "enabled"),
                    )
                  }
                >
                  <SelectTrigger id="loadbalance-strategy-auto-recovery-mode" className="w-full">
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
              </div>

              {enabledAutoRecovery ? (
                <>
                  <div className="grid gap-4 rounded-md border bg-muted/20 p-4 sm:grid-cols-2">
                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-base-open-seconds">
                        {dialogMessages.baseCooldownLabel}
                      </Label>
                      <Input
                        id="circuit-breaker-base-open-seconds"
                        type="number"
                        autoComplete="off"
                        min={0}
                        step={1}
                        value={enabledAutoRecovery.cooldown.base_seconds}
                        onChange={(event) =>
                          setLoadbalanceStrategyForm((prev) =>
                            prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
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
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-failure-threshold">
                        {dialogMessages.failureThresholdLabel}
                      </Label>
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
                            prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
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
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-backoff-multiplier">
                        {dialogMessages.backoffMultiplierLabel}
                      </Label>
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
                            prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
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
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-max-open-seconds">
                        {dialogMessages.maxCooldownLabel}
                      </Label>
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
                            prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
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
                    </div>

                    <div className="space-y-2 sm:col-span-2">
                      <Label htmlFor="circuit-breaker-jitter-ratio">
                        {dialogMessages.jitterRatioLabel}
                      </Label>
                      <Input
                        id="circuit-breaker-jitter-ratio"
                        type="number"
                        autoComplete="off"
                        min={0}
                        max={1}
                        step={0.05}
                        value={enabledAutoRecovery.cooldown.jitter_ratio}
                        onChange={(event) =>
                          setLoadbalanceStrategyForm((prev) =>
                            prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
                              ? prev
                              : {
                                  ...prev,
                                  auto_recovery: {
                                    ...prev.auto_recovery,
                                    cooldown: {
                                      ...prev.auto_recovery.cooldown,
                                      jitter_ratio: parseNumericInput(
                                        event.target.value,
                                        prev.auto_recovery.cooldown.jitter_ratio,
                                      ),
                                    },
                                  },
                                },
                          )
                        }
                      />
                    </div>
                  </div>

                  <div className="space-y-3 rounded-md border bg-muted/20 p-4">
                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-status-code-input">
                        {dialogMessages.failureStatusCodesLabel}
                      </Label>
                      <div className="flex gap-2">
                        <Input
                          id="circuit-breaker-status-code-input"
                          inputMode="numeric"
                          autoComplete="off"
                          value={enabledAutoRecovery.status_code_input}
                          onChange={(event) =>
                            setLoadbalanceStrategyForm((prev) =>
                              prev.strategy_type !== "legacy" || prev.auto_recovery.mode !== "enabled"
                                ? prev
                                : {
                                    ...prev,
                                    auto_recovery: {
                                      ...prev.auto_recovery,
                                      status_code_input: event.target.value,
                                    },
                                  },
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
                    </div>

                    <div className="flex flex-wrap gap-2">
                      {enabledAutoRecovery.status_codes.map((statusCode) => (
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
                  </div>

                  <div className="space-y-4 rounded-md border bg-muted/20 p-4">
                    <div className="space-y-2">
                      <Label htmlFor="circuit-breaker-ban-mode">{dialogMessages.banModeLabel}</Label>
                      <Select
                        value={enabledAutoRecovery.ban.mode}
                        onValueChange={(value) =>
                          setLoadbalanceStrategyForm((prev) =>
                            setLoadbalanceStrategyBanMode(prev, value as "off" | "manual" | "temporary"),
                          )
                        }
                      >
                        <SelectTrigger id="circuit-breaker-ban-mode" className="w-full">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="off">{dialogMessages.banModeOffOption}</SelectItem>
                          <SelectItem value="manual">{dialogMessages.banModeManualOption}</SelectItem>
                          <SelectItem value="temporary">{dialogMessages.banModeTemporaryOption}</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>

                    {enabledAutoRecovery.ban.mode !== "off" ? (
                      <div className="grid gap-4 sm:grid-cols-2">
                        <div className="space-y-2">
                          <Label htmlFor="circuit-breaker-max-open-strikes-before-ban">
                            {dialogMessages.maxCooldownStrikesBeforeBanLabel}
                          </Label>
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
                        </div>

                        {enabledAutoRecovery.ban.mode === "temporary" ? (
                          <div className="space-y-2">
                            <Label htmlFor="circuit-breaker-ban-duration-seconds">
                              {dialogMessages.banDurationLabel}
                            </Label>
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
                          </div>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                </>
              ) : null}
            </>
          ) : adaptiveForm ? (
            <div className="space-y-4 rounded-md border bg-muted/20 p-4">
              <div className="space-y-2">
                <Label htmlFor="adaptive-routing-policy">{dialogMessages.routingPolicyLabel}</Label>
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
                              routing_objective: value as (typeof LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES)[number],
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
              </div>
            </div>
          ) : null}

          <DialogFooter>
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
