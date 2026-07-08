import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { setSharedEndpoints } from "@/lib/referenceData";
import type { Endpoint } from "@/lib/types";

interface UseEndpointReorderOptions {
  endpoints: Endpoint[];
  revision: number;
  setEndpoints: Dispatch<SetStateAction<Endpoint[]>>;
  filtersActive: boolean;
}

export function useEndpointReorder({
  endpoints,
  revision,
  setEndpoints,
  filtersActive,
}: UseEndpointReorderOptions) {
  const [reorderInFlight, setReorderInFlight] = useState(false);
  const reorderInFlightRef = useRef(false);
  const canReorder = endpoints.length > 1 && !reorderInFlight && !filtersActive;

  const moveEndpoint = async (id: number, toIndex: number) => {
    if (!canReorder || reorderInFlightRef.current) {
      return;
    }

    const previousEndpoints = endpoints;
    const fromIndex = previousEndpoints.findIndex((endpoint) => endpoint.id === id);

    if (fromIndex === -1 || toIndex < 0 || toIndex >= previousEndpoints.length || fromIndex === toIndex) {
      return;
    }

    const nextEndpoints = previousEndpoints.slice();
    const [movedEndpoint] = nextEndpoints.splice(fromIndex, 1);

    if (!movedEndpoint) {
      return;
    }

    nextEndpoints.splice(toIndex, 0, movedEndpoint);
    reorderInFlightRef.current = true;
    setReorderInFlight(true);
    setEndpoints(nextEndpoints);
    setSharedEndpoints(revision, nextEndpoints);

    try {
      const orderedEndpoints = await api.endpoints.movePosition(Number(id), toIndex);
      setEndpoints(orderedEndpoints);
      setSharedEndpoints(revision, orderedEndpoints);
    } catch (error) {
      setEndpoints(previousEndpoints);
      setSharedEndpoints(revision, previousEndpoints);
      toast.error(
        error instanceof Error
          ? error.message
          : getStaticMessages().endpointsData.reorderedFailed,
      );
    } finally {
      reorderInFlightRef.current = false;
      setReorderInFlight(false);
    }
  };

  const moveUp = (id: number) => {
    const index = endpoints.findIndex((endpoint) => endpoint.id === id);
    if (index > 0) {
      return moveEndpoint(id, index - 1);
    }

    return Promise.resolve();
  };

  const moveDown = (id: number) => {
    const index = endpoints.findIndex((endpoint) => endpoint.id === id);
    if (index !== -1 && index < endpoints.length - 1) {
      return moveEndpoint(id, index + 1);
    }

    return Promise.resolve();
  };

  return {
    canReorder,
    moveDown,
    moveUp,
    reorderInFlight,
  };
}
