export const OBSERVE_PRESETS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type ObservePreset = (typeof OBSERVE_PRESETS)[number];

export function isObservePreset(value: string): value is ObservePreset {
  return (OBSERVE_PRESETS as readonly string[]).includes(value);
}

export type ObserveMetric = "requests" | "errors" | "ttft" | "tokens" | "cost";
export type ObserveGroupBy = "none" | "model" | "endpoint" | "terminal_target";

export const OBSERVE_METRICS: ObserveMetric[] = ["requests", "errors", "ttft", "tokens", "cost"];
export const OBSERVE_GROUPS: ObserveGroupBy[] = ["none", "model", "endpoint", "terminal_target"];

export function isObserveMetric(value: string): value is ObserveMetric {
  return (OBSERVE_METRICS as string[]).includes(value);
}

export function isObserveGroupBy(value: string): value is ObserveGroupBy {
  return (OBSERVE_GROUPS as string[]).includes(value);
}
