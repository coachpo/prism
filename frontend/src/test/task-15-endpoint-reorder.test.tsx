import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { http, HttpResponse } from "msw"
import { beforeEach, describe, expect, it } from "vitest"
import { EndpointsFeaturePage } from "@/features/endpoints/EndpointsFeaturePage"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import { clearSharedReferenceData } from "@/lib/referenceData"
import { clearUserTimezonePreference } from "@/lib/timezone"
import type { Endpoint, EndpointReferenceDetail } from "@/lib/types"
import { rewriteTestServer } from "./msw/server"

function endpoint(id: number, name: string, hasKey = true): Endpoint {
  return {
    id,
    name,
    profile_id: 1,
    base_url: `https://${name.toLowerCase()}.example.test`,
    has_api_key: hasKey,
    api_key_fingerprint: hasKey ? "fp_v1_ab12cd34ef56" : null,
    api_key_updated_at: hasKey ? "2026-01-01T00:00:00Z" : null,
    config_revision: 1,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  }
}

function referenceDetail(overrides?: Partial<EndpointReferenceDetail>): EndpointReferenceDetail {
  return {
    endpoint_id: 1,
    summary: { direct_reference_count: 0, referencing_model_count: 0, enabled_reference_count: 0, orphan_reference_count: 0 },
    reference_page: { items: [], total_count: 0, next_cursor: null, reference_snapshot_hash: "opaque-hash" },
    ...overrides,
  }
}

function renderEndpointsPage() {
  render(
    <LocaleProvider>
      <EndpointsFeaturePage />
    </LocaleProvider>,
  )
}

describe("Endpoint direct-reference contract", () => {
  beforeEach(() => {
    clearSharedReferenceData()
    clearUserTimezonePreference()
  })

  it("renders the compact table with fingerprint identity and no move controls", async () => {
    const primary = endpoint(1, "Primary")
    const backup = endpoint(2, "Backup")

    rewriteTestServer.use(
      http.get("/api/endpoints", () => HttpResponse.json([primary, backup])),
      http.get("/api/settings/costing", () => HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: "UTC", updated_at: "2026-01-01T00:00:00Z" })),
      http.post("/api/endpoints/references/batch", () =>
        HttpResponse.json({
          items: [
            { endpoint_id: 1, summary: { direct_reference_count: 0, referencing_model_count: 0, enabled_reference_count: 0, orphan_reference_count: 0 } },
            { endpoint_id: 2, summary: { direct_reference_count: 2, referencing_model_count: 1, enabled_reference_count: 1, orphan_reference_count: 0 } },
          ],
        }),
      ),
    )

    renderEndpointsPage()

    expect(await screen.findAllByText("Primary")).not.toHaveLength(0)
    expect(screen.getAllByText("Backup").length).toBeGreaterThan(0)
    // Fingerprint identity is visible.
    expect(screen.getAllByText("fp_v1_ab12cd34ef56").length).toBeGreaterThan(0)
    // No move controls exist in the new table.
    expect(screen.queryByRole("button", { name: /上移端点/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /下移端点/ })).not.toBeInTheDocument()
    // Zero direct references render as explicit zero, never as loading failure.
    expect(await screen.findAllByText("无直接引用")).not.toHaveLength(0)
  })

  it("fails closed when the references batch returns 503: no fake zero, no delete confirm", async () => {
    const primary = endpoint(1, "Primary")

    rewriteTestServer.use(
      http.get("/api/endpoints", () => HttpResponse.json([primary])),
      http.get("/api/settings/costing", () => HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: "UTC", updated_at: "2026-01-01T00:00:00Z" })),
      http.post("/api/endpoints/references/batch", () => HttpResponse.json({ detail: "upstream unavailable" }, { status: 503 })),
      http.get("/api/endpoints/1/references", () => HttpResponse.json(referenceDetail(), { status: 503 })),
      http.delete("/api/endpoints/1", () => HttpResponse.json({ deleted: true })),
    )

    renderEndpointsPage()

    // Endpoint identity still renders.
    expect(await screen.findAllByText("Primary")).not.toHaveLength(0)
    // No fake zero: the reference cell reports failure, not "无直接引用".
    await waitFor(() => {
      expect(screen.getAllByText("引用未知").length).toBeGreaterThan(0)
    })
    expect(screen.queryByText("无直接引用")).not.toBeInTheDocument()
    // The failing row carries its own recovery instead of only a page-wide one.
    expect(screen.getAllByRole("button", { name: "重试本行" }).length).toBeGreaterThan(0)

    // Delete opens the preflight but cannot confirm: check error keeps the
    // destructive submit hidden.
    const table = screen.getByTestId("endpoints-table-desktop")
    await userEvent.click(within(table).getByRole("button", { name: /删除端点/ }))
    await waitFor(() => {
      expect(screen.queryByTestId("delete-endpoint-confirm")).not.toBeInTheDocument()
    })
    expect(screen.getByText("无法检查引用状态，请重试。")).toBeInTheDocument()
  })

  it("opens a fresh preflight on delete and blocks when references exist", async () => {
    const primary = endpoint(1, "Primary")
    const detail = referenceDetail({
      summary: { direct_reference_count: 1, referencing_model_count: 1, enabled_reference_count: 1, orphan_reference_count: 0 },
      reference_page: {
        items: [
          {
            kind: "owned_terminal_target",
            connection_id: 15,
            terminal_target_id: 15,
            terminal_target_name: "Primary",
            api_family: "openai",
            connection_is_active: true,
            access_target: { id: 42, position: 0, is_enabled: true },
            owner_model: { id: 7, model_id: "gpt-example", display_name: "Example", is_enabled: true, openai_accepted_format: "dual_native", openai_image_operations: null },
            openai_text_capability: "dual_native",
            openai_image_capability: null,
            has_routing_schedule: false,
            pricing_template: { id: 2, name: "Default", current_revision_id: "944", current_version: 3, template_kind: "standard" },
            enabled: true,
            inactive_reasons: [],
          },
        ],
        total_count: 1,
        next_cursor: null,
        reference_snapshot_hash: "opaque-hash",
      },
    })

    rewriteTestServer.use(
      http.get("/api/endpoints", () => HttpResponse.json([primary])),
      http.get("/api/settings/costing", () => HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: "UTC", updated_at: "2026-01-01T00:00:00Z" })),
      http.post("/api/endpoints/references/batch", () =>
        HttpResponse.json({ items: [{ endpoint_id: 1, summary: detail.summary }] }),
      ),
      http.get("/api/endpoints/1/references", () => HttpResponse.json(detail)),
      http.delete("/api/endpoints/1", () => HttpResponse.json({ detail: "in use" }, { status: 409 })),
    )

    renderEndpointsPage()

    await screen.findAllByText("Primary")
    const table = screen.getByTestId("endpoints-table-desktop")
    await userEvent.click(within(table).getByRole("button", { name: /删除端点/ }))
    expect(await screen.findByTestId("delete-blocked-heading")).toBeInTheDocument()
    const blockers = screen.getByTestId("delete-blockers")
    expect(within(blockers).getByText("Primary")).toBeInTheDocument()
    expect(screen.queryByTestId("delete-endpoint-confirm")).not.toBeInTheDocument()
  })

  it("allows delete confirmation only after a fresh zero-reference preflight", async () => {
    const primary = endpoint(1, "Primary")
    const zeroDetail = referenceDetail()

    rewriteTestServer.use(
      http.get("/api/endpoints", () => HttpResponse.json([primary])),
      http.get("/api/settings/costing", () => HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: "UTC", updated_at: "2026-01-01T00:00:00Z" })),
      http.post("/api/endpoints/references/batch", () =>
        HttpResponse.json({ items: [{ endpoint_id: 1, summary: zeroDetail.summary }] }),
      ),
      http.get("/api/endpoints/1/references", () => HttpResponse.json(zeroDetail)),
      http.delete("/api/endpoints/1", () => HttpResponse.json({ deleted: true })),
    )

    renderEndpointsPage()

    await screen.findAllByText("Primary")
    const table = screen.getByTestId("endpoints-table-desktop")
    await userEvent.click(within(table).getByRole("button", { name: /删除端点/ }))
    await waitFor(() => {
      expect(screen.getByTestId("delete-endpoint-confirm")).toBeInTheDocument()
    })
    await userEvent.click(screen.getByTestId("delete-endpoint-confirm"))
    await waitFor(() => {
      expect(screen.queryByTestId("delete-endpoint-confirm")).not.toBeInTheDocument()
    })
  })
})
