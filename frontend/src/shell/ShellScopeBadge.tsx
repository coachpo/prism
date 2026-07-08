import { Badge } from "@/components/ui/badge"

export type ShellScopeBadgeKind = "global" | "selected-profile"

const SCOPE_BADGE_LABELS = {
  global: "Global",
  "selected-profile": "Default profile",
} as const satisfies Record<ShellScopeBadgeKind, string>

export function ShellScopeBadge({ kind }: { kind: ShellScopeBadgeKind }) {
  return (
    <Badge variant={kind === "global" ? "secondary" : "outline"} className="shrink-0">
      {SCOPE_BADGE_LABELS[kind]}
    </Badge>
  )
}
