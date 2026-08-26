import { useCallback, useState } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { Endpoint } from "@/lib/types";
import type { EndpointReferenceController } from "./useEndpointReferences";

type EndpointDuplicationReferences = Pick<
  EndpointReferenceController,
  "addEndpoint"
>;

type EndpointDuplicationOptions = {
  commitEndpoints: (updater: (current: Endpoint[]) => Endpoint[]) => void;
  references: EndpointDuplicationReferences;
  onEndpointDuplicated: (endpoint: Endpoint) => void;
};

export function useEndpointDuplication({
  commitEndpoints,
  references,
  onEndpointDuplicated,
}: EndpointDuplicationOptions) {
  const { addEndpoint } = references;
  const [duplicatingEndpointId, setDuplicatingEndpointId] = useState<
    number | null
  >(null);

  const handleDuplicateEndpoint = useCallback(
    async (endpoint: Endpoint) => {
      const messages = getStaticMessages();
      setDuplicatingEndpointId(endpoint.id);
      try {
        const duplicate = await api.endpoints.duplicate(endpoint.id);
        toast.success(messages.endpointsData.duplicatedAs(duplicate.name));
        commitEndpoints((current) => [...current, duplicate]);
        addEndpoint(duplicate.id);
        onEndpointDuplicated(duplicate);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : messages.endpointsData.duplicateFailed,
        );
      } finally {
        setDuplicatingEndpointId(null);
      }
    },
    [addEndpoint, commitEndpoints, onEndpointDuplicated],
  );

  return { duplicatingEndpointId, handleDuplicateEndpoint };
}
