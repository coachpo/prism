import { useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorSectionCard } from "@/shared/design-system";
import { useModelCatalog } from "@/pages/model-detail/useModelCatalog";
import { ModelsDevCatalogPanel } from "../detail/ModelsDevCatalogPanel";
import type { OpenCodeExportModelRow } from "@/lib/types";

// Keep the export session mounted while the existing CAS owner edits metadata.
// Saving never patches export facts; both the binding and source are re-read.
export function OpenCodeRepairPanel({ model, pending, failed, onRefresh, onClose }: {
  model: OpenCodeExportModelRow;
  pending: boolean;
  failed: boolean;
  onRefresh: () => void;
  onClose: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.clientReadiness;
  const catalog = useModelCatalog(model.model_config_id, 0);
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    panel.current?.focus();
    panel.current?.scrollIntoView({ block: "nearest" });
  }, []);
  return (
    <div ref={panel} tabIndex={-1} role="region" aria-label={copy.repairTitle}>
      <OperatorSectionCard
        title={copy.repairTitle}
        description={copy.repairHint}
        actions={<Button variant="outline" onClick={onClose} disabled={pending}>{copy.back}</Button>}
      >
        <p className="font-mono">{model.model_id}</p>
        {pending ? (
          <OperatorCallout intent="muted" description={copy.rechecking} />
        ) : !failed && model.readiness?.status === "ready" ? (
          <OperatorCallout intent="success" description={copy.recovered} />
        ) : null}
        <ModelsDevCatalogPanel
          modelConfigId={model.model_config_id}
          prismModelId={model.model_id}
          apiFamily={model.api_family}
          catalogView={catalog}
          onChanged={() => { catalog.refresh(); onRefresh(); }}
        />
      </OperatorSectionCard>
    </div>
  );
}
