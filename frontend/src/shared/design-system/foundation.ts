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
  "default",
  "neutral",
  "muted",
  "accent",
  "info",
  "success",
  "warning",
  "danger",
  "healthy",
  "downgrade",
  "unhealthy",
] as const

export type OperatorStatusIntent = (typeof operatorStatusIntents)[number]

export const operatorTokenContract = {
  color: {
    base: ["background", "foreground", "card", "popover", "primary", "secondary", "muted", "accent", "destructive"],
    status: ["success", "healthy", "warning", "downgrade", "info", "unhealthy"],
    charts: ["chart-1", "chart-2", "chart-3", "chart-4", "chart-5"],
    shell: ["sidebar", "sidebar-accent", "operator-surface", "command"],
  },
  typography: {
    families: ["--operator-font-sans", "--operator-font-mono"],
    tracking: "--tracking-operator",
    numeric: "tabular-nums",
  },
  spacing: {
    page: ["--density-page-gap", "--density-page-pad-x", "--density-page-pad-y"],
    card: ["--density-card-gap", "--density-card-pad-x", "--density-card-pad-y"],
    controls: ["--density-control-h", "--density-control-h-sm", "--density-control-h-xs"],
    table: ["--density-table-head-h", "--density-table-cell-px", "--density-table-cell-py"],
  },
  radius: ["--radius-sm", "--radius-md", "--radius-lg", "--radius-xl"],
  shadows: ["--shadow-operator-panel", "--shadow-operator-glow"],
  motion: ["operator-page-transition", "ws-new-row", "ws-value-updated"],
  state: ["focus-visible:ring-ring", "aria-invalid:border-destructive", "disabled:opacity-50"],
} as const

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
    "management-system-density-first",
    "semantic-token-colors",
    "primitive-composition-before-custom-markup",
    "no-motion-gated-functionality",
    "shared-operator-components-before-page-local-patterns",
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
