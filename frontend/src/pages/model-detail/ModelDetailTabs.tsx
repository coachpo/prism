import type { ComponentProps } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useLocale } from "@/i18n/useLocale";
import { ConnectionsList } from "./ConnectionsList";
import { LoadbalanceEventsTab } from "./LoadbalanceEventsTab";

type ConnectionsListProps = ComponentProps<typeof ConnectionsList>;

interface ModelDetailTabsProps extends ConnectionsListProps {
  activeTab: "connections" | "events";
  setActiveTab: (value: "connections" | "events") => void;
}

export function ModelDetailTabs({ activeTab, setActiveTab, ...connectionsListProps }: ModelDetailTabsProps) {
  const { messages } = useLocale();

  return (
    <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as "connections" | "events")} className="space-y-4">
      <TabsList className="grid h-11 w-full max-w-md grid-cols-2 rounded-xl bg-muted/70 p-1">
        <TabsTrigger value="connections" className="rounded-lg text-sm font-medium">
          {messages.modelDetailTabs.connections}
        </TabsTrigger>
        <TabsTrigger value="events" className="rounded-lg text-sm font-medium">
          {messages.modelDetailTabs.loadbalanceEvents}
        </TabsTrigger>
      </TabsList>

      <TabsContent value="connections" className="mt-0 space-y-4">
        <ConnectionsList {...connectionsListProps} />
      </TabsContent>

      <TabsContent value="events" className="mt-0 space-y-4">
        <LoadbalanceEventsTab modelId={connectionsListProps.model.model_id} />
      </TabsContent>
    </Tabs>
  );
}
