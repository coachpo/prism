"use client"

import * as React from "react"

export type SidebarState = "expanded" | "collapsed"

export type SidebarContextProps = {
  state: SidebarState
  open: boolean
  setOpen: (open: boolean) => void
  openMobile: boolean
  setOpenMobile: (open: boolean) => void
  isMobile: boolean
  toggleSidebar: () => void
}

export const SidebarContext = React.createContext<SidebarContextProps | null>(null)

export function useSidebarContext() {
  return React.useContext(SidebarContext)
}

export function useSidebar() {
  const context = useSidebarContext()
  if (!context) {
    throw new Error("useSidebar must be used within a SidebarProvider.")
  }

  return context
}
