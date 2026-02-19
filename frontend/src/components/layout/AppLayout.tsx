import { NavLink, Outlet } from "react-router-dom";
import { cn } from "@/lib/utils";
import { LayoutDashboard, Server, Zap } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";

export function AppLayout() {
  return (
    <div className="flex h-screen w-full bg-background text-foreground">
      {/* Sidebar — always dark-toned via sidebar CSS vars */}
      <aside className="fixed left-0 top-0 z-30 flex h-full w-[240px] flex-col bg-sidebar text-sidebar-foreground">
        {/* Brand */}
        <div className="flex h-14 items-center gap-2 border-b border-sidebar-border px-5">
          <div className="flex h-7 w-7 items-center justify-center rounded-md bg-sidebar-primary">
            <Zap className="h-4 w-4 text-sidebar-primary-foreground" />
          </div>
          <h1 className="text-base font-semibold tracking-tight">LLM Gateway</h1>
        </div>

        {/* Nav */}
        <nav className="flex-1 space-y-1 p-3">
          <NavLink
            to="/dashboard"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                isActive
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/60 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
              )
            }
          >
            <LayoutDashboard className="h-4 w-4" />
            Dashboard
          </NavLink>
          <NavLink
            to="/models"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                isActive
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/60 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
              )
            }
          >
            <Server className="h-4 w-4" />
            Models
          </NavLink>
        </nav>

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-sidebar-border px-4 py-3">
          <span className="text-xs text-sidebar-foreground/40">v1.0</span>
          <ThemeToggle />
        </div>
      </aside>

      {/* Main Content */}
      <main className="ml-[240px] flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl px-8 py-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
