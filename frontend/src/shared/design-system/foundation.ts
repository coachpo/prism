export const requiredShadcnPrimitives = [
  "sidebar",
  "table",
  "form",
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

/** Two density modes, switchable from the header via `data-density` on <html>. */
export const operatorDensityModes = ["standard", "compact"] as const

export type OperatorDensityMode = (typeof operatorDensityModes)[number]

export const operatorDefaultDensityMode: OperatorDensityMode = "standard"

/**
 * Runtime status, four mutually exclusive tiers. Anything describing how a
 * thing is behaving right now resolves to exactly one of these.
 */
export const operatorStatusTiers = ["healthy", "degraded", "failing", "idle"] as const

export type OperatorStatusTier = (typeof operatorStatusTiers)[number]

/** Status is never color alone: each tier also carries a shape marker. */
export const operatorStatusMarkers: Record<OperatorStatusTier, string> = {
  healthy: "●",
  degraded: "◐",
  failing: "▲",
  idle: "○",
}

/**
 * Badge tones. The four runtime tiers plus non-runtime tones for categories,
 * raw values, and irreversible operations. `danger` marks destructive actions
 * only and never describes runtime state.
 */
export const operatorBadgeIntents = [
  "default",
  "neutral",
  "muted",
  "accent",
  "danger",
  ...operatorStatusTiers,
] as const

export type OperatorBadgeIntent = (typeof operatorBadgeIntents)[number]

/** Notice severity for `OperatorCallout`. A different axis from runtime status. */
export const operatorCalloutIntents = ["info", "success", "warning", "danger", "muted"] as const

export type OperatorCalloutIntent = (typeof operatorCalloutIntents)[number]

/**
 * The token contract, as data.
 *
 * `foundation.test.ts` reads `src/index.css` and fails when a declared token is
 * missing, when a defined color token is declared nowhere here (dead token), or
 * when a measured pair falls under its required contrast ratio. Colors are
 * never approved by eye.
 */
export type OperatorColorToken = {
  /** CSS custom property name without the leading dashes. */
  name: string
  light: string
  dark: string
  /**
   * Surface this token is measured against, or `null` when the token is itself
   * a surface or a hairline and carries no foreground contrast requirement.
   */
  against: "panel" | "canvas" | null
  /** WCAG 2.1 minimum. 4.5 for anything carrying text, 3 for graphics. */
  minContrast: number | null
  role: "surface" | "hairline" | "text" | "primary" | "status" | "spectrum" | "decoration"
}

export const operatorColorTokens: readonly OperatorColorToken[] = [
  // Surfaces
  { name: "canvas", light: "#f6f7f9", dark: "#0f1319", against: null, minContrast: null, role: "surface" },
  { name: "panel", light: "#ffffff", dark: "#161b22", against: null, minContrast: null, role: "surface" },
  { name: "raised", light: "#ffffff", dark: "#1c232c", against: null, minContrast: null, role: "surface" },
  { name: "inset", light: "#f0f2f5", dark: "#11161d", against: null, minContrast: null, role: "surface" },
  { name: "border", light: "#dde1e7", dark: "#2a323d", against: null, minContrast: null, role: "hairline" },
  { name: "border-strong", light: "#c3c9d2", dark: "#3a4552", against: null, minContrast: null, role: "hairline" },

  // Text — two informative tiers
  { name: "text", light: "#11161d", dark: "#e6e9ee", against: "panel", minContrast: 4.5, role: "text" },
  { name: "text-muted", light: "#5a6472", dark: "#98a2b3", against: "panel", minContrast: 4.5, role: "text" },
  // Demoted to decoration: 3.10:1 in light, and it cannot be darkened without
  // collapsing into text-muted. Disabled controls and >=24px glyphs only.
  { name: "text-disabled", light: "#8a93a1", dark: "#6e7885", against: "panel", minContrast: null, role: "decoration" },

  // Primary — the incident beam
  { name: "primary", light: "#1e4fd8", dark: "#9fbeff", against: "panel", minContrast: 4.5, role: "primary" },
  { name: "on-primary", light: "#ffffff", dark: "#04266e", against: null, minContrast: null, role: "primary" },
  { name: "primary-soft", light: "#e4ebff", dark: "#1b2e63", against: null, minContrast: null, role: "primary" },
  { name: "on-primary-soft", light: "#123a9e", dark: "#cfddff", against: null, minContrast: null, role: "primary" },

  // Runtime status — four tiers
  { name: "healthy", light: "#0f7b4f", dark: "#66d9a0", against: "panel", minContrast: 4.5, role: "status" },
  { name: "degraded", light: "#8c5200", dark: "#f5c063", against: "panel", minContrast: 4.5, role: "status" },
  { name: "failing", light: "#c0342b", dark: "#ff8a80", against: "panel", minContrast: 4.5, role: "status" },
  { name: "idle", light: "#5b6370", dark: "#8a93a1", against: "panel", minContrast: 4.5, role: "status" },
  { name: "destructive", light: "#c0342b", dark: "#ff6b61", against: "panel", minContrast: 4.5, role: "status" },
  // Text on a filled destructive button. Measured against `destructive`, not
  // `panel`, so it carries no panel floor of its own.
  { name: "on-destructive", light: "#ffffff", dark: "#2b0705", against: null, minContrast: null, role: "status" },

  // Spectrum — categorical data encoding, graphics threshold
  { name: "spectrum-1", light: "#1e4fd8", dark: "#9fbeff", against: "panel", minContrast: 3, role: "spectrum" },
  { name: "spectrum-2", light: "#0b7285", dark: "#5fd3e8", against: "panel", minContrast: 3, role: "spectrum" },
  { name: "spectrum-3", light: "#0f7b4f", dark: "#66d9a0", against: "panel", minContrast: 3, role: "spectrum" },
  { name: "spectrum-4", light: "#a56200", dark: "#f5c063", against: "panel", minContrast: 3, role: "spectrum" },
  { name: "spectrum-5", light: "#b22d6e", dark: "#f58ab8", against: "panel", minContrast: 3, role: "spectrum" },
  { name: "spectrum-6", light: "#6741d9", dark: "#b197fc", against: "panel", minContrast: 3, role: "spectrum" },
]

/**
 * shadcn primitives in `src/components/ui` read these variable names directly.
 * They are aliases onto the tokens above, not a second palette, so the guard
 * test allows them in `index.css` while requiring each to resolve to a
 * `var(--token)` reference rather than a literal color.
 */
export const shadcnAliasTokens = [
  "background",
  "foreground",
  "card",
  "card-foreground",
  "popover",
  "popover-foreground",
  "primary-foreground",
  "secondary",
  "secondary-foreground",
  "muted",
  "muted-foreground",
  "accent",
  "accent-foreground",
  "input",
  "ring",
  "chart-1",
  "chart-2",
  "chart-3",
  "chart-4",
  "chart-5",
  "chart-6",
  "sidebar",
  "sidebar-foreground",
  "sidebar-primary",
  "sidebar-primary-foreground",
  "sidebar-accent",
  "sidebar-accent-foreground",
  "sidebar-border",
  "sidebar-ring",
] as const

/** Radius scale. 8px card base; large radii read as loose in a dense console. */
export const operatorRadiusScale = {
  badge: "var(--radius-sm)",
  control: "var(--radius-md)",
  card: "var(--radius-lg)",
  dialog: "var(--radius-xl)",
} as const

export const operatorDensityVariables = {
  page: ["--density-page-gap", "--density-page-pad-x", "--density-page-pad-y"],
  card: ["--density-card-gap", "--density-card-pad-x", "--density-card-pad-y"],
  controls: ["--density-control-h", "--density-control-h-sm", "--density-control-h-xs"],
  table: [
    "--density-table-head-h",
    "--density-table-row-h",
    "--density-table-cell-px",
    "--density-table-cell-py",
  ],
} as const

export const operatorTypographyRoles = [
  "page-title",
  "section-title",
  "card-title",
  "body",
  "caption",
  "label",
  "table-header",
  "table-cell",
  "kpi-value",
  "code",
] as const

export const operatorPrimitiveInventory: Record<RequiredShadcnPrimitive, "present"> = {
  sidebar: "present",
  table: "present",
  form: "present",
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
  tone: "operator-cockpit",
  typography: {
    display: "var(--font-sans)",
    mono: "var(--font-mono)",
    tracking: "var(--tracking-operator)",
  },
  density: {
    defaultMode: operatorDefaultDensityMode,
    tableCellX: "var(--density-table-cell-px)",
    tableCellY: "var(--density-table-cell-py)",
  },
  guardrails: [
    "honesty-before-tidiness",
    "density-before-whitespace",
    "status-never-by-color-alone",
    "numbers-in-mono-tabular",
    "outline-layering-not-shadow",
    "semantic-token-colors",
    "primitive-composition-before-custom-markup",
    "no-motion-gated-functionality",
    "shared-operator-components-before-page-local-patterns",
  ],
} as const

export const operatorTableShellContract = {
  usesPrimitive: "@/components/ui/table",
  densityAttribute: "data-density",
  emptyStatePrimitive: "@/components/ui/empty",
  loadingStatePrimitive: "@/components/ui/skeleton",
} as const

/** WCAG 2.1 relative luminance. Exported so the guard test and callers agree. */
export function relativeLuminance(hex: string): number {
  const value = hex.replace("#", "")
  const full =
    value.length === 3
      ? value
          .split("")
          .map((c) => c + c)
          .join("")
      : value
  const channels = [0, 2, 4].map((offset) => {
    const srgb = Number.parseInt(full.slice(offset, offset + 2), 16) / 255
    return srgb <= 0.04045 ? srgb / 12.92 : ((srgb + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}

export function contrastRatio(foreground: string, background: string): number {
  const a = relativeLuminance(foreground)
  const b = relativeLuminance(background)
  const [lighter, darker] = a > b ? [a, b] : [b, a]
  return (lighter + 0.05) / (darker + 0.05)
}
