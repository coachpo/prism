import { useCallback, useEffect, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import {
  getRequestLogAuditErrorMessage,
  IDLE_DETAIL,
  type DetailLaneState,
  type ListLaneState,
} from "./requestLogAuditLanes";

interface UseRequestLogAuditDetailInput {
  list: ListLaneState;
  selectedAuditId: number | null;
  selectedAuditParamLabel: string | null;
  selectedAuditParamPresent: boolean;
}

export function useRequestLogAuditDetail({
  list,
  selectedAuditId,
  selectedAuditParamLabel,
  selectedAuditParamPresent,
}: UseRequestLogAuditDetailInput) {
  const messages = getStaticMessages();
  const [detail, setDetail] = useState<DetailLaneState>(IDLE_DETAIL);
  const [detailNonce, setDetailNonce] = useState(0);
  const resolvedSelection =
    list.phase !== "ready"
      ? null
      : !selectedAuditParamPresent
        ? list.items[0] ?? null
        : selectedAuditId === null
          ? null
          : list.items.find((item) => item.id === selectedAuditId) ?? null;

  useEffect(() => {
    if (list.phase === "loading") {
      setDetail((current) => ({ ...current, phase: "idle", detail: null }));
      return;
    }
    if (list.phase !== "ready" || list.items.length === 0) {
      setDetail(IDLE_DETAIL);
      return;
    }
    if (!selectedAuditParamPresent) {
      if (!resolvedSelection) {
        setDetail(IDLE_DETAIL);
        return;
      }
    } else if (selectedAuditId !== null && !resolvedSelection) {
      setDetail({
        phase: "missing_selection",
        detail: null,
        selectedAuditId: null,
        missingAuditLabel: selectedAuditParamLabel,
        error: null,
      });
      return;
    } else if (!resolvedSelection) {
      setDetail(IDLE_DETAIL);
      return;
    }

    const selected = resolvedSelection;
    let cancelled = false;
    setDetail({
      phase: "loading",
      detail: null,
      selectedAuditId: selected.id,
      missingAuditLabel: null,
      error: null,
    });
    void (async () => {
      try {
        const response = await api.audit.get(selected.id);
        if (cancelled) return;
        setDetail({
          phase: "ready",
          detail: response,
          selectedAuditId: selected.id,
          missingAuditLabel: null,
          error: null,
        });
      } catch (error) {
        if (cancelled) return;
        setDetail({
          phase: "error",
          detail: null,
          selectedAuditId: selected.id,
          missingAuditLabel: null,
          error: getRequestLogAuditErrorMessage(
            error,
            messages.requestLogs.auditDetailLoadFailed,
          ),
        });
      }
    })();
    return () => {
      cancelled = true;
    };
    // The selected row is the only detail-read identity. The list owner keeps
    // cursor replacement and page errors out of this request lane.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    list.items,
    list.phase,
    messages.requestLogs.auditDetailLoadFailed,
    resolvedSelection?.id,
    selectedAuditId,
    selectedAuditParamLabel,
    selectedAuditParamPresent,
    detailNonce,
  ]);

  const retryDetail = useCallback(() => {
    setDetailNonce((current) => current + 1);
  }, []);

  return { detail, retryDetail };
}
