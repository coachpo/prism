export type RewriteShellScope = "global" | "selected-profile" | "runtime"

export type RewriteShellNavItem = {
  path: string
  label: string
  scope: RewriteShellScope
}

export const rewriteShellNavItems = [
  { path: "/dashboard", label: "Dashboard", scope: "selected-profile" },
  { path: "/models", label: "Models", scope: "selected-profile" },
  { path: "/sidecars", label: "Sidecars", scope: "global" },
  { path: "/observe/requests", label: "Request Logs", scope: "selected-profile" },
] as const satisfies readonly RewriteShellNavItem[]
