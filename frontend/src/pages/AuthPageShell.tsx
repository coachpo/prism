import type { ReactNode } from "react"

import { ThemeToggle } from "@/components/ThemeToggle"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

type AuthPageShellProps = {
  title: ReactNode
  description: ReactNode
  children: ReactNode
}

export function AuthPageShell({
  children,
  description,
  title,
}: AuthPageShellProps) {
  return (
    <main className="min-h-svh bg-background px-4 py-4 text-foreground sm:px-6 sm:py-6">
      <div className="mx-auto flex w-full max-w-6xl items-center justify-between gap-4">
        <div className="rounded-full border border-outline-variant bg-surface px-3 py-1 text-xs font-medium shadow-operator-panel">
          Prism
        </div>
        <div className="flex items-center gap-2">
          <ThemeToggle
            buttonClassName="size-9 rounded-full border border-outline-variant bg-surface text-foreground hover:bg-surface-container-low"
            menuClassName="border-outline-variant bg-popover"
          />
        </div>
      </div>

      <div className="mx-auto flex min-h-[calc(100svh-5rem)] w-full max-w-md items-center justify-center py-8">
        <Card className="operator-section-surface w-full overflow-hidden">
          <CardHeader className="border-b">
            <CardTitle className="text-2xl font-semibold tracking-tight">{title}</CardTitle>
            <CardDescription className="leading-6">{description}</CardDescription>
          </CardHeader>
          <CardContent>{children}</CardContent>
        </Card>
      </div>
    </main>
  )
}
