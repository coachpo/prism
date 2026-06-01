export const TARGET_COMPATIBILITY_GLOSSARY = {
  accessTargetTypes: {
    model: "model",
    terminalTarget: "connection",
  },
  compatibilityFields: {
    terminalTargetId: "connection_id",
    terminalTargetObject: "connection",
    ownerScopedTerminalTargetRoute: "/api/models/{model_config_id}/connections",
  },
  additiveFields: {
    terminalTargetId: "terminal_target_id",
    terminalTargetObject: "terminal_target",
    terminalTargetProductKind: "product_kind",
  },
} as const;

export type PersistedTerminalTargetType =
  typeof TARGET_COMPATIBILITY_GLOSSARY.accessTargetTypes.terminalTarget;

export type TerminalTargetProductKind = "terminal_target";

export type CompatibilityTerminalTargetKind =
  | PersistedTerminalTargetType
  | TerminalTargetProductKind;

export function isTerminalTargetAccessTargetType(
  value: string | null | undefined,
): value is PersistedTerminalTargetType {
  return value === TARGET_COMPATIBILITY_GLOSSARY.accessTargetTypes.terminalTarget;
}

export function isCompatibilityTerminalTargetKind(
  value: string | null | undefined,
): value is CompatibilityTerminalTargetKind {
  return value === TARGET_COMPATIBILITY_GLOSSARY.accessTargetTypes.terminalTarget || value === "terminal_target";
}

type TerminalTargetIdCarrier = {
  terminal_target_id?: number | null;
  connection_id?: number | null;
};

export function getTerminalTargetId(value: TerminalTargetIdCarrier): number | null {
  return value.terminal_target_id ?? value.connection_id ?? null;
}

type TerminalTargetCarrier<T> = {
  terminal_target?: T | null;
  connection?: T | null;
};

export function getTerminalTarget<T>(value: TerminalTargetCarrier<T>): T | null {
  return value.terminal_target ?? value.connection ?? null;
}

type TerminalTargetProductKindCarrier = {
  product_kind?: TerminalTargetProductKind | null;
  kind?: string | null;
};

export function getTerminalTargetProductKind(
  value: TerminalTargetProductKindCarrier,
): TerminalTargetProductKind | null {
  if (value.product_kind === "terminal_target") {
    return value.product_kind;
  }

  return isCompatibilityTerminalTargetKind(value.kind) ? "terminal_target" : null;
}

type ActiveTerminalTargetCountCarrier = {
  activeTerminalTargetCount?: number;
  activeConnectionCount?: number;
};

export function getActiveTerminalTargetCount(
  value: ActiveTerminalTargetCountCarrier,
): number {
  return value.activeTerminalTargetCount ?? value.activeConnectionCount ?? 0;
}

type ActiveTerminalTargetTotalCarrier = {
  activeTerminalTargetTotal?: number;
  activeConnectionTotal?: number;
};

export function getActiveTerminalTargetTotal(
  value: ActiveTerminalTargetTotalCarrier,
): number {
  return value.activeTerminalTargetTotal ?? value.activeConnectionTotal ?? 0;
}
