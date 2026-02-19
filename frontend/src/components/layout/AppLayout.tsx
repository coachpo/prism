import { NavLink, Outlet } from "react-router-dom";
import { cn } from "@/lib/utils";
import { LayoutDashboard, Server } from "lucide-react";

export function AppLayout() {
  return (
    <div className="flex h-screen w-full bg-background text-foreground">
      {/* Sidebar */}
      <aside className="fixed left-0 top-0 z-30 h-full w-[240px] border-r border-border bg-card">
        <div className="flex h-14 items-center border-b border-border px-6">
          <h1 className="text-lg font-semibold tracking-tight">LLM Gateway</h1>
        </div>
        <nav className="space-y-1 p-4">
          <NavLink
            to="/dashboard"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
                isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground"
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
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
                isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground"
              )
            }
          >
            <Server className="h-4 w-4" />
            Models
          </NavLink>
        </nav>
      </aside>

      {/* Main Content */}
      <main className="ml-[240px] flex-1 overflow-y-auto p-8">
        <div className="mx-auto max-w-6xl space-y-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
