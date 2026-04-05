import { useState } from "react";

export function useModelDetailPageShell(navigate: (to: string) => void) {
  const [activeTab, setActiveTab] = useState<"connections" | "events">("connections");

  return {
    activeTab,
    setActiveTab,
    navigateBackToModels: () => navigate("/models"),
    navigateToRequestLogs: (modelId: string) =>
      navigate(`/request-logs?model_id=${encodeURIComponent(modelId)}`),
  };
}
