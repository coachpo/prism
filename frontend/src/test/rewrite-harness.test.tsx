import { QueryClientProvider, useQuery } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { createRewriteQueryClient, rewriteProfileScopeSchema, rewriteRoutePaths } from "@/app"
import { rewriteShellNavItems } from "@/shell"
import { rewriteQueryKeys, rewriteTableColumns } from "@/shared"

function HarnessProbe() {
  const query = useQuery({
    queryKey: rewriteQueryKeys.contract(),
    queryFn: async () => {
      const response = await fetch("/api/rewrite-harness/health")
      return response.json() as Promise<{ source: string; status: string }>
    },
  })

  return (
    <button type="button">
      {query.data ? `${query.data.status}:${query.data.source}` : "loading"}
    </button>
  )
}

describe("rewrite Vitest harness", () => {
  it("loads alias-based architecture contracts and MSW-backed query setup", async () => {
    const queryClient = createRewriteQueryClient()

    render(
      <QueryClientProvider client={queryClient}>
        <HarnessProbe />
      </QueryClientProvider>,
    )

    const button = await screen.findByRole("button", {
      name: "ok:msw-test-harness",
    })
    await userEvent.click(button)

    expect(rewriteRoutePaths).toContain("/sidecars")
    expect(rewriteShellNavItems.some((item) => item.path === "/sidecars" && item.scope === "global")).toBe(true)
    expect(rewriteProfileScopeSchema.safeParse({ profileId: crypto.randomUUID(), reason: "contract" }).success).toBe(true)
    expect(rewriteTableColumns[0].header).toBe("Label")
  })
})
