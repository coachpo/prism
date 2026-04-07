import { api } from "@/lib/api";
import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

type ConnectionOwner = Awaited<ReturnType<typeof api.connections.owner>>;

interface CreateConnectionNavigatorOptions {
  navigate: (to: string) => void;
  selectedProfileId: number | null;
}

export function createConnectionNavigator({
  navigate,
  selectedProfileId: _selectedProfileId,
}: CreateConnectionNavigatorOptions) {
  let navigating = false;

  return async (connectionId: number) => {
    if (navigating) {
      return;
    }

    navigating = true;

    try {
      const owner: ConnectionOwner = await api.connections.owner(connectionId);

      navigate(`/models/${owner.model_config_id}?focus_connection_id=${connectionId}`);
    } catch {
      toast.error(getMessages().requestLogsDetail.connectionNotFound);
    } finally {
      navigating = false;
    }
  };
}
