import { useCallback, useState } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { isReferenceIntegrityError } from "@/lib/api/endpointErrors";
import type { EndpointReferenceItem } from "@/lib/types";
import type { OrphanCleanupEndpoint } from "@/pages/endpoints/OrphanCleanupDialog";
import type { EndpointReferenceController } from "./useEndpointReferences";

type EndpointOrphanReferences = Pick<
  EndpointReferenceController,
  "invalidateEndpoint"
>;

export function useEndpointOrphanCleanup({
  references,
}: {
  references: EndpointOrphanReferences;
}) {
  const { invalidateEndpoint } = references;
  const [orphanCleanupTarget, setOrphanCleanupTarget] = useState<{
    endpoint: OrphanCleanupEndpoint;
    item: EndpointReferenceItem;
  } | null>(null);

  const handleOrphanCleanup = useCallback(
    async (endpoint: OrphanCleanupEndpoint, item: EndpointReferenceItem) => {
      const messages = getStaticMessages();
      try {
        await api.endpoints.orphanCleanup(endpoint.id, item.connection_id);
        toast.success(messages.endpointsData.orphanCleaned);
        setOrphanCleanupTarget(null);
        invalidateEndpoint(endpoint.id);
      } catch (error) {
        if (isReferenceIntegrityError(error)) {
          toast.error(messages.endpointsData.orphanCleanupFailed);
          invalidateEndpoint(endpoint.id);
          return;
        }
        toast.error(
          error instanceof Error
            ? error.message
            : messages.endpointsData.orphanCleanupFailed,
        );
      }
    },
    [invalidateEndpoint],
  );

  return {
    handleOrphanCleanup,
    orphanCleanupTarget,
    setOrphanCleanupTarget,
  };
}
