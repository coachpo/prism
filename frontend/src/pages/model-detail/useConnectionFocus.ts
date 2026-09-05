import { useEffect, useRef } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import type { Dispatch, SetStateAction } from "react";
import type { ModelConfig } from "@/lib/types";

type SetURLSearchParams = (next: URLSearchParams, options?: { replace?: boolean }) => void;

interface UseConnectionFocusInput {
  model: ModelConfig | null;
  searchParams: URLSearchParams;
  setSearchParams: SetURLSearchParams;
  connectionCardRefs: Map<number, HTMLElement>;
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

    let cancelled = false;
    let animationFrameId: number | null = null;
    let attempts = 0;

    const clearFocusParam = () => {
      const nextSearchParams = new URLSearchParams(searchParams);
      nextSearchParams.delete("focus_connection_id");
      setSearchParams(nextSearchParams, { replace: true });
    };

    const focusConnectionCard = () => {
      if (cancelled) return;

      const element = connectionCardRefs.get(connectionId);
      if (!element) {
        attempts += 1;
        if (attempts >= 30) {
          // 找不到就说出来：默默吞掉参数会让人以为「跳过来什么也没发生」。
          setFocusedConnectionId(null);
          toast.error(
            getStaticMessages().modelDetail.focusTargetNotFound(
              String(connectionId),
            ),
          );
          clearFocusParam();
          return;
        }

        animationFrameId = window.requestAnimationFrame(focusConnectionCard);
        return;
      }

      // 参数只在定位成功后才移除，否则刷新页面就再也回不到这一行。
      clearFocusParam();
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
