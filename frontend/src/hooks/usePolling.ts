import { useCallback, useEffect, useRef, useState } from "react";

export interface UsePollingOptions {
  /** Callback to execute on each poll interval */
  onPoll: () => void | Promise<void>;
  /** Polling interval in milliseconds (default: 30000) */
  interval?: number;
  /** Whether polling is enabled (default: true) */
  enabled?: boolean;
}

export interface UsePollingReturn {
  /** Whether polling is currently active */
  isPolling: boolean;
  /** Manually trigger a poll immediately */
  manualRefresh: () => void;
}

/**
 * Hook that implements periodic polling with page visibility detection.
 * Uses recursive setTimeout to prevent request piling.
 * Automatically pauses when page is hidden.
 *
 * @example
 * const { isPolling, manualRefresh } = usePolling({
 *   onPoll: async () => {
 *     const data = await fetchStats();
 *     setStats(data);
 *   },
 *   interval: 30000,
 *   enabled: true,
 * });
 */
export function usePolling({
  onPoll,
  interval = 30000,
  enabled = true,
}: UsePollingOptions): UsePollingReturn {
  const [isPolling, setIsPolling] = useState(false);
  const timeoutRef = useRef<NodeJS.Timeout | null>(null);
  const onPollRef = useRef(onPoll);
  const isPollingInFlightRef = useRef(false);

  useEffect(() => {
    onPollRef.current = onPoll;
  }, [onPoll]);

  const isPageVisible = useCallback(() => {
    return document.visibilityState === "visible";
  }, []);

  const schedulePollRef = useRef<() => void>(() => {});

  const schedulePoll = useCallback(() => {
    if (!enabled) {
      setIsPolling(false);
      return;
    }

    if (!isPageVisible()) {
      setIsPolling(false);
      return;
    }

    setIsPolling(true);

    timeoutRef.current = setTimeout(async () => {
      if (isPollingInFlightRef.current) {
        schedulePollRef.current();
        return;
      }

      isPollingInFlightRef.current = true;
      try {
        await onPollRef.current();
      } catch (error) {
        console.error("Polling error:", error);
      } finally {
        isPollingInFlightRef.current = false;
      }

      schedulePollRef.current();
    }, interval);
  }, [enabled, interval, isPageVisible]);

  useEffect(() => {
    schedulePollRef.current = schedulePoll;
  }, [schedulePoll]);

  const handleVisibilityChange = useCallback(() => {
    if (document.visibilityState === "hidden") {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
      setIsPolling(false);
    } else {
      if (enabled) {
        schedulePoll();
      }
    }
  }, [enabled, schedulePoll]);

  const manualRefresh = useCallback(async () => {
    if (isPollingInFlightRef.current) {
      return;
    }

    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
    
    isPollingInFlightRef.current = true;
    try {
      await onPollRef.current();
    } catch (error) {
      console.error("Manual refresh error:", error);
    } finally {
      isPollingInFlightRef.current = false;
    }
    
    if (enabled && isPageVisible()) {
      schedulePoll();
    }
  }, [enabled, isPageVisible, schedulePoll]);

  useEffect(() => {
    if (!enabled) {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
      setIsPolling(false);
      return;
    }

    if (isPageVisible()) {
      schedulePoll();
    }

    document.addEventListener("visibilitychange", handleVisibilityChange);

    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
      setIsPolling(false);
    };
  }, [enabled, isPageVisible, schedulePoll, handleVisibilityChange]);

  return {
    isPolling,
    manualRefresh,
  };
}
