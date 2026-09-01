import { useState } from "react";

import {
  upstreamModelIdIssueFromError,
  upstreamModelIdIssueMessage,
  validateUpstreamModelIdField,
} from "@/pages/model-detail/upstreamModelIdField";

/** Owns the create dialog's follow-until-edited upstream identity lifecycle. */
export function useInitialTerminalTargetUpstreamModelId({
  modelId,
}: {
  modelId: string;
}) {
  const [operatorValue, setOperatorValue] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const value = operatorValue ?? modelId;

  const updateFromOperator = (nextValue: string) => {
    setError(null);
    setOperatorValue(nextValue);
  };

  const validate = (): boolean => {
    const issue = validateUpstreamModelIdField(value, true);
    if (!issue) {
      setError(null);
      return true;
    }
    setError(upstreamModelIdIssueMessage(issue, "create"));
    return false;
  };

  const applyServerError = (caught: unknown): boolean => {
    const issue = upstreamModelIdIssueFromError(caught);
    if (!issue) return false;
    setError(upstreamModelIdIssueMessage(issue, "create"));
    return true;
  };

  return {
    value,
    error,
    reset: () => {
      setOperatorValue(null);
      setError(null);
    },
    clearError: () => setError(null),
    updateFromOperator,
    validate,
    applyServerError,
  };
}
