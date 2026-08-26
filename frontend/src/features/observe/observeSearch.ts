export const OBSERVE_PRESETS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type ObservePreset = (typeof OBSERVE_PRESETS)[number];

export function isObservePreset(value: string): value is ObservePreset {
  return (OBSERVE_PRESETS as readonly string[]).includes(value);
}

/** UI order is fixed: requests, errors, TTFT, output rate, tokens, cache read, cost. */
export const OBSERVE_METRICS = [
  "requests",
  "errors",
  "ttft",
  "output_rate",
  "tokens",
  "cache_read_share",
  "cost",
] as const;
export type ObserveMetric = (typeof OBSERVE_METRICS)[number];

export const OBSERVE_GROUPS = [
  "none",
  "model",
  "endpoint",
  "terminal_target",
] as const;
export type ObserveGroupBy = (typeof OBSERVE_GROUPS)[number];

export function isObserveMetric(value: string): value is ObserveMetric {
  return (OBSERVE_METRICS as readonly string[]).includes(value);
}

export function isObserveGroupBy(value: string): value is ObserveGroupBy {
  return (OBSERVE_GROUPS as readonly string[]).includes(value);
}
