export const requiredShadcnPrimitives = [
  "sidebar",
  "table",
  "form",
  "command",
  "chart",
  "dialog",
  "alert-dialog",
  "sheet",
  "drawer",
  "sonner",
  "empty",
  "skeleton",
  "badge",
] as const

export type RequiredShadcnPrimitive = (typeof requiredShadcnPrimitives)[number]

export const operatorDensityModes = ["compact", "balanced", "expanded"] as const

export type OperatorDensityMode = (typeof operatorDensityModes)[number]

export const operatorStatusIntents = [
  "neutral",
  "info",
  "success",
  "warning",
  "danger",
  "healthy",
  "downgrade",
  "unhealthy",
] as const

export type OperatorStatusIntent = (typeof operatorStatusIntents)[number]

export const operatorPrimitiveInventory: Record<RequiredShadcnPrimitive, "present"> = {
  sidebar: "present",
  table: "present",
  form: "present",
  command: "present",
  chart: "present",
  dialog: "present",
  "alert-dialog": "present",
  sheet: "present",
  drawer: "present",
  sonner: "present",
  empty: "present",
  skeleton: "present",
  badge: "present",
}
export const operatorDesignFoundation = {
  tone: "operator-control-room",
  typography: {
    display: "var(--font-sans)",
    mono: "var(--font-mono)",
    tracking: "var(--tracking-operator)",
  },
  density: {
    defaultMode: "balanced" satisfies OperatorDensityMode,
    tableCellX: "var(--density-table-cell-px)",
    tableCellY: "var(--density-table-cell-py)",
    commandInput: "var(--density-command-input-h)",
  },
  guardrails: [
    "cinematic-but-restrained",
    "no-generic-card-grid-theming",
    "no-cheap-meta-labels",
    "no-motion-gated-functionality",
    "grid-flow-dense-for-bento-surfaces",
  ],
} as const

export const operatorCommandPaletteContract = {
  triggerTestId: "command-palette-trigger",
  titleRequired: true,
  primitive: "@/components/ui/command",
} as const

export const operatorTableShellContract = {
  usesPrimitive: "@/components/ui/table",
  densityAttribute: "data-density",
  emptyStatePrimitive: "@/components/ui/empty",
  loadingStatePrimitive: "@/components/ui/skeleton",
} as const
