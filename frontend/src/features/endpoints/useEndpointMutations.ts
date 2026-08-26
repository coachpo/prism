import { useEndpointAttachment } from "./useEndpointAttachment";
import { useEndpointDeletion } from "./useEndpointDeletion";
import { useEndpointDuplication } from "./useEndpointDuplication";
import { useEndpointFormMutations } from "./useEndpointFormMutations";
import { useEndpointOrphanCleanup } from "./useEndpointOrphanCleanup";
import type { EndpointReferenceController } from "./useEndpointReferences";
import type { Endpoint } from "@/lib/types";

type EndpointMutationOptions = {
  endpoints: Endpoint[];
  commitEndpoints: (updater: (current: Endpoint[]) => Endpoint[]) => void;
  references: EndpointReferenceController;
};

/**
 * Page-level mutation composition. Each resource lifecycle owns its own
 * state; this hook only wires the shared Endpoint DTO/reference handoffs and
 * the post-create/duplicate attach presentation.
 */
export function useEndpointMutations({
  endpoints,
  commitEndpoints,
  references,
}: EndpointMutationOptions) {
  const attachment = useEndpointAttachment();
  const forms = useEndpointFormMutations({
    commitEndpoints,
    onEndpointCreated: attachment.setAttachModelTarget,
    references,
  });
  const deletion = useEndpointDeletion({
    commitEndpoints,
    endpoints,
    references,
  });
  const duplication = useEndpointDuplication({
    commitEndpoints,
    onEndpointDuplicated: attachment.setAttachModelTarget,
    references,
  });
  const orphanCleanup = useEndpointOrphanCleanup({ references });

  return {
    ...attachment,
    ...deletion,
    ...duplication,
    ...forms,
    ...orphanCleanup,
  };
}
