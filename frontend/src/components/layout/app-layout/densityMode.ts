import { useCallback, useEffect, useState } from "react";

import {
  operatorDefaultDensityMode,
  operatorDensityModes,
  type OperatorDensityMode,
} from "@/shared/design-system";

/**
 * Versioned on its own key rather than folded into an existing preference
 * blob, so a future density change bumps `.v2` instead of rewriting a shape
 * other readers already depend on.
 */
export const DENSITY_MODE_STORAGE_SLOT = "prism.density.v1";

const DENSITY_ATTRIBUTE = "data-density";

function isDensityMode(value: unknown): value is OperatorDensityMode {
  return operatorDensityModes.includes(value as OperatorDensityMode);
}

export function readDensityMode(): OperatorDensityMode {
  if (typeof window === "undefined") return operatorDefaultDensityMode;
  try {
    const stored = window.localStorage?.getItem(DENSITY_MODE_STORAGE_SLOT);
    return isDensityMode(stored) ? stored : operatorDefaultDensityMode;
  } catch {
    return operatorDefaultDensityMode;
  }
}

export function writeDensityMode(mode: OperatorDensityMode): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage?.setItem(DENSITY_MODE_STORAGE_SLOT, mode);
  } catch {
    // A blocked storage must not break the switch itself.
  }
}

export function applyDensityMode(mode: OperatorDensityMode): void {
  if (typeof document === "undefined") return;
  document.documentElement.setAttribute(DENSITY_ATTRIBUTE, mode);
}

export function useDensityMode() {
  const [mode, setMode] = useState<OperatorDensityMode>(readDensityMode);

  useEffect(() => {
    applyDensityMode(mode);
    writeDensityMode(mode);
  }, [mode]);

  const toggle = useCallback(() => {
    setMode((current) => (current === "compact" ? "standard" : "compact"));
  }, []);

  return { mode, setMode, toggle };
}
