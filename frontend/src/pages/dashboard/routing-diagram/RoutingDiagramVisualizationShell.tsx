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
    <section className="operator-section-surface flex flex-col gap-4 rounded-xl border border-outline-variant p-4 sm:p-5">
      <div className="flex flex-col gap-4">
        {children}
        {visualization}
      </div>
    </section>
  );
}
