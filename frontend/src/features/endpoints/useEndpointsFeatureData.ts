import { useEndpointList } from "./useEndpointList";
import { useEndpointMutations } from "./useEndpointMutations";

/**
 * Page composition root for the Endpoint feature. List/reference hydration
 * and Endpoint mutation workflows keep their state in their owning hooks;
 * this hook only wires their shared resource handoff for the page.
 */
export function useEndpointsFeatureData() {
  const list = useEndpointList();
  const mutations = useEndpointMutations({
    endpoints: list.endpoints,
    commitEndpoints: list.commitEndpoints,
    references: list.references,
  });

  return {
    ...list,
    ...mutations,
  };
}
