import { useMemo } from "react";
import { Route } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyTarget } from "@/lib/types";
import { normalizeProxyTargets } from "../models/modelFormState";

interface ProxyTargetsCardProps {
  availableTargets: { modelId: string; label: string }[];
  proxyTargets: ProxyTarget[];
}

export function ProxyTargetsCard({ availableTargets, proxyTargets }: ProxyTargetsCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelsUi;
  const detailCopy = messages.modelDetail;
  const normalizedTargets = useMemo(() => normalizeProxyTargets(proxyTargets), [proxyTargets]);

  const resolveTargetLabel = (targetModelId: string) => {
    return availableTargets.find((target) => target.modelId === targetModelId)?.label ?? targetModelId;
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Route className="h-4 w-4" />
          {detailCopy.proxyTargets}
        </CardTitle>
        <CardDescription>{detailCopy.orderedPriorityRouting}</CardDescription>
        <p className="text-sm text-muted-foreground">{copy.proxyTargetsDescriptionPrimary}</p>
      </CardHeader>
      <CardContent className="space-y-3">
        {normalizedTargets.length === 0 ? (
          <div className="rounded-md border border-dashed border-border px-3 py-4 text-sm text-muted-foreground">
            {copy.noProxyTargetsSelected}
          </div>
        ) : (
          <div className="space-y-2">
            {normalizedTargets.map((target, index) => (
              <div key={target.target_model_id} className="rounded-md border px-3 py-2">
                <p className="truncate text-sm font-medium">{resolveTargetLabel(target.target_model_id)}</p>
                <p className="text-xs text-muted-foreground">{copy.priority(formatNumber(index + 1))}</p>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
