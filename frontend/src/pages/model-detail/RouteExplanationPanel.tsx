import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectGroup, SelectItem } from "@/components/ui/select";
import { OperatorErrorState, OperatorSectionCard, OperatorStalenessBadge } from "@/shared/design-system";
import { routeExplanation } from "@/lib/api";
import type { RouteExplanation } from "@/lib/types";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { buildModelDetailPath } from "@/app/router/rewriteRoutes";
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance";
import { routeExplanationOperations } from "./routeExplanationOperations";

type RouteExplanationPanelProps = { modelId: number; apiFamily: string };

export function RouteExplanationPanel(props: RouteExplanationPanelProps) {
  const epoch = useSyncExternalStore(
    (listener) => authSessionCoordinator.subscribe(listener),
    () => authSessionCoordinator.getEpoch(),
  );
  return <RouteExplanationSession key={`${epoch}:${props.modelId}:${props.apiFamily}`} {...props} />;
}

// Completed and in-flight samples belong to one model/family/auth epoch. A
// keyed session drops both even when the surrounding detail page stays mounted.
function RouteExplanationSession({ modelId, apiFamily }: RouteExplanationPanelProps) {
  const { messages } = useLocale();
  const copy = messages.routeExplanation;
  const options = routeExplanationOperations(apiFamily, messages);
  const [operation, setOperation] = useState(options[0]?.[0] ?? "");
  const [data, setData] = useState<RouteExplanation | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const { formatWithZone } = useTimezone();
  const fieldId = useId();
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => { controller.current?.abort(); }, []);
  const changeOperation = (value: string) => {
    controller.current?.abort();
    setOperation(value);
    setData(null);
    setError(null);
    setPending(false);
  };
  const refresh = () => {
    if (pending || !operation) return;
    const abort = new AbortController();
    controller.current = abort;
    setPending(true);
    setError(null);
    routeExplanation.get(modelId, operation, abort.signal).then((result) => {
      if (!abort.signal.aborted) setData(result);
    }).catch((cause: unknown) => {
      if (!abort.signal.aborted) setError(cause instanceof Error ? cause.message : copy.readFailed);
    }).finally(() => { if (!abort.signal.aborted) setPending(false); });
  };
  const strategies: Record<string, string> = {
    single: messages.observe.strategySingle,
    "fill-first": messages.observe.strategyFillFirst,
    "round-robin": messages.observe.strategyRoundRobin,
  };
  return (
    <OperatorSectionCard
      title={copy.title}
      description={copy.description}
      contentClassName="flex flex-col gap-2"
      actions={<Button disabled={pending || !operation} onClick={refresh}>{pending ? copy.pending : copy.refresh}</Button>}
    >
      <Field>
        <FieldLabel htmlFor={fieldId}>{copy.operation}</FieldLabel>
        <Select value={operation} onValueChange={changeOperation}>
          <SelectTrigger id={fieldId} aria-label={copy.operationLabel}><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {options.map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
            </SelectGroup>
          </SelectContent>
        </Select>
      </Field>
      {error ? <OperatorErrorState title={copy.failed} description={error} /> : null}
      {error && data ? <OperatorStalenessBadge label={copy.stale(formatWithZone(data.observed_at))} reason={error} /> : null}
      {!data && !error ? <p className="text-sm text-muted-foreground">{pending ? copy.loading : copy.idle}</p> : null}
      {data ? <div className="flex flex-col gap-2 text-sm">
        <p>{copy.sample(formatWithZone(data.observed_at), formatWithZone(data.sample_completed_at), data.generation, formatWithZone(data.published_at))} · {data.completeness === "partial" ? copy.partial : copy.sampled}</p>
        <p className="text-muted-foreground">{copy.boundary}</p>
        {data.planner_error ? <OperatorErrorState title={copy.plannerFailed} description={data.planner_error} /> : null}
        {data.candidates.map((row, index) => <div className="border-b py-2" key={`${row.terminal_target_id}-${index}`}>
          <Link className="text-primary underline" to={buildModelDetailPath(row.model_config_id)}>{copy.terminalPath(row.path.join(" → "), row.terminal_target_id)}</Link>
          <p>{strategies[row.strategy] ?? copy.unknownStrategy} · {row.planner_position === null ? copy.excluded : copy.candidate(row.planner_position)} · {copy.reasons[row.reason] ?? copy.unknownReason} · {copy.schedules[row.schedule] ?? copy.unknownSchedule}</p>
          <p className="text-muted-foreground">{row.runtime_observed ? copy.observed : copy.unobserved} · {row.capacity === "sampled_limit_reached" ? copy.limited : row.capacity === "sampled_not_reserved" ? copy.unreserved : copy.missingCapacity}</p>
        </div>)}
        {data.exclusions?.map((row, index) => <p key={`excluded-${index}`}><Link className="text-primary underline" to={buildModelDetailPath(row.model_config_id)}>{copy.repairPath(row.path.filter(Boolean).join(" → "))}</Link>：{row.reason === "target_disabled" ? copy.disabled : copy.unavailable}</p>)}
        {data.candidates.length === 0 && !data.exclusions?.length ? <p>{copy.empty}</p> : null}
      </div> : null}
    </OperatorSectionCard>
  );
}
