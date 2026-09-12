import { useEffect, useState } from "react";
import { getSharedModels } from "@/lib/referenceData";

/** Persisted model names identify the historical route; current collection IDs identify configuration pages. */
export function useRequestRecoveryModel(modelName: string, enabled: boolean) {
  const [resolved, setResolved] = useState<{ name: string; id: number } | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    void getSharedModels(0).then(models => {
      const matches = models.filter(model => model.model_id === modelName);
      if (active) setResolved(matches.length === 1 ? { name: modelName, id: matches[0].id } : null);
    }).catch(() => { if (active) setResolved(null); });
    return () => { active = false; };
  }, [modelName, enabled]);
  return resolved?.name === modelName ? resolved.id : null;
}
