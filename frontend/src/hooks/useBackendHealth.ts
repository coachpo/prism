import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { BackendHealthResponse } from "@/lib/types";

export function useBackendHealth() {
  const [health, setHealth] = useState<BackendHealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);

    try {
      const response = await api.health.get();
      setHealth(response);
      setError(false);
    } catch {
      setHealth(null);
      setError(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return {
    error,
    health,
    loading,
    refresh,
  };
}
