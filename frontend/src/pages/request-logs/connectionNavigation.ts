import { api } from "@/lib/api";
import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

type ConnectionReferences = Awaited<ReturnType<typeof api.connections.references>>;

interface CreateConnectionNavigatorOptions {
  navigate: (to: string) => void;
}

export function createConnectionNavigator({
  navigate,
}: CreateConnectionNavigatorOptions) {
  let navigating = false;

  return async (connectionId: number) => {
    if (navigating) {
      return;
    }

    navigating = true;

    try {
      const references: ConnectionReferences = await api.connections.references(connectionId);
      const owner = references.items[0];

      if (!owner) {
        toast.error(getMessages().requestLogsDetail.connectionNotFound);
        return;
      }

      navigate(`/models/${owner.model_config_id}?focus_connection_id=${connectionId}`);
    } catch {
      toast.error(getMessages().requestLogsDetail.connectionNotFound);
    } finally {
      navigating = false;
    }
  };
}
