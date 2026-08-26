import type { ReactNode } from "react"

import { ThemeToggle } from "@/components/ThemeToggle"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { VERSION_LABEL } from "@/lib/appVersion"
import { useLocale } from "@/i18n/useLocale"

type AuthPageShellProps = {
  title: ReactNode
  description: ReactNode
  children: ReactNode
  /** Banner slot directly under the card header, above the body. */
  banner?: ReactNode
  /** Mono facts row pinned to the card footer. */
  facts?: ReactNode
}

/**
 * Self-hosted installs run several gateways. Before typing a password an
 * operator needs to know which one this is, so the shell states the host and
 * the build rather than only the product name.
 */
export function AuthPageShell({
  banner,
  children,
  description,
  facts,
  title,
}: AuthPageShellProps) {
  const { messages } = useLocale()
  const host = typeof window === "undefined" ? "" : window.location.host

  return (
    <main className="min-h-svh bg-canvas px-4 py-4 text-foreground sm:px-6 sm:py-6">
      <div className="mx-auto flex w-full max-w-6xl items-center justify-between gap-4">
        <div className="flex items-center gap-2 rounded-md border border-border bg-panel px-2.5 py-1">
          <span
            aria-hidden="true"
            className="flex size-5 items-center justify-center rounded-[4px] bg-primary text-[10px] font-semibold text-on-primary"
          >
            P
          </span>
          <span className="text-xs font-semibold">Prism</span>
          {host ? (
            <span className="font-mono text-[11px] text-muted-foreground" title={`${messages.auth.instanceLabel}: ${host}`}>
              {host}
            </span>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <span className="font-mono text-[11px] text-muted-foreground" title={messages.shell.version}>
            {VERSION_LABEL}
          </span>
          <ThemeToggle
            buttonClassName="size-8 rounded-md border border-border bg-panel text-foreground hover:bg-inset"
            menuClassName="border-border bg-popover"
          />
        </div>
      </div>

      <div className="mx-auto flex min-h-[calc(100svh-5rem)] w-full max-w-md items-center justify-center py-8">
        <Card className="operator-section-surface w-full gap-0 overflow-hidden">
          <CardHeader className="border-b pb-4">
            <CardTitle className="text-[1.375rem] font-semibold leading-7 tracking-tight">{title}</CardTitle>
            <CardDescription className="text-[0.8125rem] leading-5">{description}</CardDescription>
          </CardHeader>
          {banner ? <div className="border-b bg-inset px-[var(--density-card-pad-x)] py-3">{banner}</div> : null}
          <CardContent className="pt-4">{children}</CardContent>
          {facts ? (
            <CardFooter className="border-t bg-inset py-2">
              <div className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] text-muted-foreground">
                {facts}
              </div>
            </CardFooter>
          ) : null}
        </Card>
      </div>
    </main>
  )
}
