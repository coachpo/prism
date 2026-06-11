import { useState } from "react";

export function useModelDetailPageShell(navigate: (to: string) => void) {
  const [activeTab, setActiveTab] = useState<"connections" | "events">("connections");

  return {
    activeTab,
    setActiveTab,
    navigateBackToModels: () => navigate("/models"),
    navigateToRequestLogs: (modelId: string) =>
      navigate(`/observe/requests?model=${encodeURIComponent(modelId)}`),
  };
}
