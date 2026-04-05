import { useEffect, useRef } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { SetURLSearchParams } from "react-router-dom";
import type { ModelConfig } from "@/lib/types";

interface UseConnectionFocusInput {
  model: ModelConfig | null;
  searchParams: URLSearchParams;
  setSearchParams: SetURLSearchParams;
  connectionCardRefs: Map<number, HTMLDivElement>;
  setFocusedConnectionId: Dispatch<SetStateAction<number | null>>;
}

export function useConnectionFocus({
  model,
  searchParams,
  setSearchParams,
  connectionCardRefs,
  setFocusedConnectionId,
}: UseConnectionFocusInput) {
  const focusTimeoutRef = useRef<number | null>(null);

  useEffect(() => {
    if (!model) return;

    const focusId = searchParams.get("focus_connection_id");
    if (!focusId) return;

    const connectionId = Number.parseInt(focusId, 10);
    if (!Number.isFinite(connectionId)) return;

    setFocusedConnectionId(connectionId);

    const nextSearchParams = new URLSearchParams(searchParams);
    nextSearchParams.delete("focus_connection_id");
    setSearchParams(nextSearchParams, { replace: true });

    let cancelled = false;
    let animationFrameId: number | null = null;
    let attempts = 0;

    const focusConnectionCard = () => {
      if (cancelled) return;

      const element = connectionCardRefs.get(connectionId);
      if (!element) {
        attempts += 1;
        if (attempts >= 30) {
          setFocusedConnectionId(null);
          return;
        }

        animationFrameId = window.requestAnimationFrame(focusConnectionCard);
        return;
      }

      element.scrollIntoView({ behavior: "smooth", block: "center" });
      element.focus({ preventScroll: true });

      if (focusTimeoutRef.current !== null) {
        window.clearTimeout(focusTimeoutRef.current);
      }

      focusTimeoutRef.current = window.setTimeout(() => {
        setFocusedConnectionId(null);
        focusTimeoutRef.current = null;
      }, 3000);
    };

    animationFrameId = window.requestAnimationFrame(focusConnectionCard);

    return () => {
      cancelled = true;
      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId);
      }
      if (focusTimeoutRef.current !== null) {
        window.clearTimeout(focusTimeoutRef.current);
        focusTimeoutRef.current = null;
      }
    };
  }, [connectionCardRefs, model, searchParams, setFocusedConnectionId, setSearchParams]);
}
