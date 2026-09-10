import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { AuditLogDetail } from "@/lib/types";

export type ComparisonCapture = {
  detail: AuditLogDetail | null;
  error: string | null;
  loading: boolean;
};
const EMPTY: ComparisonCapture = { detail: null, error: null, loading: false };

/** Two independent, explicit reads. Cancellation also rejects APIs that resolve after abort. */
export function useRequestComparisonCapture(ids: [string, string]) {
  const [captures, setCaptures] = useState<
    [ComparisonCapture, ComparisonCapture]
  >([EMPTY, EMPTY]);
  const controllers = useRef<[AbortController | null, AbortController | null]>([
    null,
    null,
  ]);
  useEffect(() => {
    const active = controllers.current;
    return () => {
      for (const controller of active) controller?.abort();
    };
  }, []);
  const cancel = (index: number) => {
    controllers.current[index]?.abort();
    controllers.current[index] = null;
    setCaptures(
      (current) =>
        current.map((value, i) =>
          i === index ? EMPTY : value,
        ) as typeof current,
    );
  };
  const load = async (index: number, auditId: number) => {
    controllers.current[index]?.abort();
    const controller = new AbortController();
    controllers.current[index] = controller;
    setCaptures(
      (current) =>
        current.map((value, i) =>
          i === index ? { ...EMPTY, loading: true } : value,
        ) as typeof current,
    );
    try {
      const detail = await api.audit.get(auditId, {
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      if (detail.request_log_id !== ids[index])
        throw new Error(getStaticMessages().requestComparison.unavailable);
      setCaptures(
        (current) =>
          current.map((value, i) =>
            i === index ? { detail, loading: false, error: null } : value,
          ) as typeof current,
      );
    } catch (error) {
      if (controller.signal.aborted) return;
      setCaptures(
        (current) =>
          current.map((value, i) =>
            i === index
              ? {
                  ...EMPTY,
                  error:
                    error instanceof Error
                      ? error.message
                      : getStaticMessages().requestComparison.unavailable,
                }
              : value,
          ) as typeof current,
      );
    }
  };
  return { captures, cancel, load };
}
