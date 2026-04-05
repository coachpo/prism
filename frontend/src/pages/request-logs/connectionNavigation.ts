import { api } from "@/lib/api";
import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

type ConnectionOwner = Awaited<ReturnType<typeof api.connections.owner>>;

const ownerCacheByProfile = new Map<number, Map<number, ConnectionOwner>>();

function getOwnerCache(selectedProfileId: number | null) {
  if (selectedProfileId === null) {
    return new Map<number, ConnectionOwner>();
  }

  const existing = ownerCacheByProfile.get(selectedProfileId);
  if (existing) {
    return existing;
  }

  const created = new Map<number, ConnectionOwner>();
  ownerCacheByProfile.set(selectedProfileId, created);
  return created;
}

interface CreateConnectionNavigatorOptions {
  navigate: (to: string) => void;
  selectedProfileId: number | null;
}

export function createConnectionNavigator({
  navigate,
  selectedProfileId,
}: CreateConnectionNavigatorOptions) {
  const ownerCache = getOwnerCache(selectedProfileId);
  let navigating = false;

  return async (connectionId: number) => {
    if (navigating) {
      return;
    }

    navigating = true;

    try {
      let owner = ownerCache.get(connectionId);
      if (!owner) {
        owner = await api.connections.owner(connectionId);
        ownerCache.set(connectionId, owner);
      }

      navigate(`/models/${owner.model_config_id}?focus_connection_id=${connectionId}`);
    } catch {
      toast.error(getMessages().requestLogsDetail.connectionNotFound);
    } finally {
      navigating = false;
    }
  };
}
