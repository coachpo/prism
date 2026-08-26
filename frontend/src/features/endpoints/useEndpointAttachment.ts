import { useCallback, useState } from "react";

import type { Endpoint } from "@/lib/types";

export function useEndpointAttachment() {
  const [attachModelTarget, setAttachModelTarget] = useState<Endpoint | null>(
    null,
  );

  const handleAttachNavigate = useCallback((endpoint: Endpoint) => {
    // One-shot attach: open the model picker; the selected model detail page
    // consumes action=create-terminal-target + endpoint_id (never key material).
    setAttachModelTarget(endpoint);
  }, []);

  const handleAttachModelSelected = useCallback(
    (modelId: number) => {
      if (!attachModelTarget) return;
      const endpoint = attachModelTarget;
      setAttachModelTarget(null);
      window.location.assign(
        `/route/models/${modelId}?action=create-terminal-target&endpoint_id=${endpoint.id}`,
      );
    },
    [attachModelTarget],
  );

  return {
    attachModelTarget,
    handleAttachModelSelected,
    handleAttachNavigate,
    setAttachModelTarget,
  };
}
