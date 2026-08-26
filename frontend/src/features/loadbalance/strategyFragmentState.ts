export type FragmentPhase = "idle" | "loading" | "ready" | "empty" | "error";

export interface FragmentState<T> {
  phase: FragmentPhase;
  data: T | null;
  stale: boolean;
  lastSuccessfulAt: string | null;
  error: string | null;
  semanticQueryKey: string;
}

export function initialStrategyFragment<T>(key: string): FragmentState<T> {
  return {
    phase: "idle",
    data: null,
    stale: false,
    lastSuccessfulAt: null,
    error: null,
    semanticQueryKey: key,
  };
}
