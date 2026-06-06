import type { ReactNode } from "react";

interface RoutingDiagramVisualizationShellProps {
  children?: ReactNode;
  visualization: ReactNode;
}

export function RoutingDiagramVisualizationShell({
  children,
  visualization,
}: RoutingDiagramVisualizationShellProps) {
  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border/70 bg-card/95 p-4 shadow-none sm:p-5">
      <div className="flex flex-col gap-4">
        {children}
        {visualization}
      </div>
    </section>
  );
}
