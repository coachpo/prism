import { useState } from "react";

import { api } from "@/lib/api";
import type { LoadbalanceCurrentStateResetResponse } from "@/lib/types";
import type { PageReadKind } from "@/shared/table/paginationStates";

interface UseRoutingHealthCurrentStateResetInput {
  applyResetSnapshot: (
    targetId: number,
    response: LoadbalanceCurrentStateResetResponse,
  ) => void;
  load: (kind: PageReadKind) => void | Promise<void>;
  resetFailedMessage: string;
  resetNothingToClearMessage: string;
}

export function useRoutingHealthCurrentStateReset({
  applyResetSnapshot,
  load,
  resetFailedMessage,
  resetNothingToClearMessage,
}: UseRoutingHealthCurrentStateResetInput) {
  const [resettingTargetId, setResettingTargetId] = useState<number | null>(
    null,
  );
  const [resetError, setResetError] = useState<string | null>(null);
  const [resetNotice, setResetNotice] = useState<string | null>(null);

  const resetTarget = async (targetId: number) => {
    setResettingTargetId(targetId);
    setResetError(null);
    setResetNotice(null);
    try {
      const response = await api.loadbalance.resetCurrentState(targetId);
      applyResetSnapshot(targetId, response);
      if (!response.cleared) setResetNotice(resetNothingToClearMessage);
      await load("refresh");
    } catch (error) {
      setResetError(
        error instanceof Error ? error.message : resetFailedMessage,
      );
    } finally {
      setResettingTargetId(null);
    }
  };

  return { resetError, resetNotice, resetTarget, resettingTargetId };
}
