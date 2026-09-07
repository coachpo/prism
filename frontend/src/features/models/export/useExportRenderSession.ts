import { useCallback, useEffect, useRef, useState } from "react";
import type { ExportRenderResponse } from "@/lib/types";
import type { KeyDecision } from "./ExportKeyDialog";

/** The ephemeral lifetime of one explicit credential decision and its result. */
export function useExportRenderSession({
  render,
  refetchSource,
  renderFailedMessage,
}: {
  render: (
    decision: KeyDecision,
    signal: AbortSignal,
  ) => Promise<ExportRenderResponse>;
  refetchSource: () => unknown;
  renderFailedMessage: string;
}) {
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [renderResult, setRenderResult] = useState<ExportRenderResponse | null>(
    null,
  );
  const [renderError, setRenderError] = useState<string | null>(null);
  const [renderStale, setRenderStale] = useState(false);
  const active = useRef<AbortController | null>(null);
  const invalidate = useCallback(() => {
    active.current?.abort();
    active.current = null;
  }, []);
  useEffect(() => invalidate, [invalidate]);

  const handleGenerate = useCallback(
    async (decision: KeyDecision) => {
      invalidate();
      const controller = new AbortController();
      active.current = controller;
      setRenderError(null);
      setRenderStale(false);
      try {
        const result = await render(decision, controller.signal);
        // Abort may race a response, or a test transport may ignore it entirely.
        if (active.current !== controller) {
          throw new DOMException("Export session closed", "AbortError");
        }
        active.current = null;
        setRenderResult(result);
      } catch (error) {
        if (active.current !== controller) throw error;
        active.current = null;
        const detail = error as { status?: number; message?: string };
        if (detail.status === 409) {
          setRenderStale(true);
          void refetchSource();
        }
        setRenderError(detail.message ?? renderFailedMessage);
        throw error;
      }
    },
    [invalidate, refetchSource, render, renderFailedMessage],
  );

  const clearResult = useCallback(() => {
    invalidate();
    setRenderResult(null);
  }, [invalidate]);
  const closeKeyDialog = useCallback(() => {
    invalidate();
    setKeyDialogOpen(false);
  }, [invalidate]);
  const openKeyDialog = useCallback(() => {
    invalidate();
    setRenderResult(null);
    setRenderError(null);
    setRenderStale(false);
    setKeyDialogOpen(true);
  }, [invalidate]);

  return {
    keyDialogOpen,
    renderResult,
    renderError,
    renderStale,
    handleGenerate,
    clearResult,
    closeKeyDialog,
    openKeyDialog,
  };
}
