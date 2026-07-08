import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { http, HttpResponse } from "msw"
import { beforeEach, describe, expect, it } from "vitest"
import { EndpointsFeaturePage } from "@/features/endpoints/EndpointsFeaturePage"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import { clearSharedReferenceData } from "@/lib/referenceData"
import { clearUserTimezonePreference } from "@/lib/timezone"
import type { Endpoint } from "@/lib/types"
import { rewriteTestServer } from "./msw/server"

function endpoint(id: number, name: string, position: number): Endpoint {
  return {
    id,
    name,
    position,
    profile_id: 1,
    base_url: `https://${name.toLowerCase()}.example.test`,
    has_api_key: true,
    masked_api_key: "sk-...test",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  }
}

function renderEndpointsPage() {
  render(
    <LocaleProvider>
      <EndpointsFeaturePage />
    </LocaleProvider>,
  )
}

describe("Task 15 endpoint reorder buttons", () => {
  beforeEach(() => {
    clearSharedReferenceData()
    clearUserTimezonePreference()
  })

  it("moves endpoints with buttons and sends the movePosition target index", async () => {
    const primary = endpoint(1, "Primary", 0)
    const backup = endpoint(2, "Backup", 1)
    let moveRequest: unknown

    rewriteTestServer.use(
      http.get("/api/endpoints", () => HttpResponse.json([primary, backup])),
      http.get("/api/settings/timezone", () => HttpResponse.json({ timezone_preference: "UTC" })),
      http.post("/api/models/by-endpoints", () => HttpResponse.json({ items: [] })),
      http.patch("/api/endpoints/:id/position", async ({ params, request }) => {
        moveRequest = {
          body: await request.json(),
          id: String(params.id),
        }

        return HttpResponse.json([
          { ...backup, position: 0 },
          { ...primary, position: 1 },
        ])
      }),
    )

    renderEndpointsPage()

    expect(await screen.findByRole("button", { name: "上移端点 Backup" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "上移端点 Primary" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "下移端点 Backup" })).toBeDisabled()

    await userEvent.click(screen.getByRole("button", { name: "上移端点 Backup" }))

    await waitFor(() => {
      expect(moveRequest).toEqual({ body: { to_index: 0 }, id: "2" })
    })
    await waitFor(() => {
      expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual([
        "Backup",
        "Primary",
      ])
    })
  })
})
