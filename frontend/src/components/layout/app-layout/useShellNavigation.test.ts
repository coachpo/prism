import { describe, expect, it } from "vitest"

import {
  SHELL_ROUTE_METADATA,
  SHELL_SIDEBAR_GROUP_ORDER,
  SHELL_SIDEBAR_ITEMS,
} from "./useShellNavigation"

describe("shell navigation contract", () => {
  it("derives routing navigation in setup dependency order", () => {
    expect(SHELL_SIDEBAR_ITEMS.filter((item) => item.groupId === "routing").map((item) => item.id)).toEqual([
      "endpoints",
      "pricing-templates",
      "loadbalance-strategies",
      "models",
    ])
  })

  it("keeps route metadata as the single source for setup targets", () => {
    expect(SHELL_ROUTE_METADATA.find((route) => route.id === "endpoints")?.sidebarItem?.to).toBe("/route/endpoints")
    expect(SHELL_ROUTE_METADATA.find((route) => route.id === "pricing-templates")?.sidebarItem?.to).toBe("/route/pricing")
    expect(SHELL_ROUTE_METADATA.find((route) => route.id === "loadbalance-strategies")?.sidebarItem?.to).toBe("/route/ban-policies")
    expect(SHELL_ROUTE_METADATA.find((route) => route.id === "models")?.sidebarItem?.to).toBe("/route/models")
    expect(SHELL_ROUTE_METADATA.find((route) => route.id === "routing-health")?.sidebarItem?.to).toBe("/observe/routing-health")
  })

  it("keeps path prefix, sidebar group, and breadcrumb group aligned", () => {
    const prefixByGroup = {
      observability: "/observe",
      routing: "/route",
      system: "/system",
    } as const

    for (const route of SHELL_ROUTE_METADATA) {
      expect(
        route.canonicalPath.startsWith(prefixByGroup[route.groupId]),
        `${route.canonicalPath} does not sit under ${prefixByGroup[route.groupId]}`,
      ).toBe(true)
    }
  })

  it("gives every sidebar item an icon and a known group", () => {
    for (const item of SHELL_SIDEBAR_ITEMS) {
      expect(item.icon, `${item.id} has no icon`).toBeTruthy()
      expect(SHELL_SIDEBAR_GROUP_ORDER).toContain(item.groupId)
    }
  })

  it("routes every non-sidebar page to an owning sidebar item", () => {
    const sidebarIds = new Set(SHELL_SIDEBAR_ITEMS.map((item) => item.id))
    for (const route of SHELL_ROUTE_METADATA) {
      expect(sidebarIds.has(route.sidebarItemId), `${route.id} points at an unknown sidebar item`).toBe(true)
    }
  })
})
