import { useMemo, useState, type Dispatch, type SetStateAction } from "react";
import {
  KeyboardSensor,
  PointerSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { arrayMove, verticalListSortingStrategy, sortableKeyboardCoordinates } from "@dnd-kit/sortable";
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
  const [activeDragId, setActiveDragId] = useState<UniqueIdentifier | null>(null);
  const [reorderInFlight, setReorderInFlight] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 250,
        tolerance: 5,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const endpointIds = useMemo(() => endpoints.map((endpoint) => endpoint.id), [endpoints]);

  const activeDragEndpoint = useMemo(
    () => endpoints.find((endpoint) => endpoint.id === activeDragId) ?? null,
    [activeDragId, endpoints],
  );

  const canReorder = endpoints.length > 1 && !reorderInFlight && !filtersActive;

  const handleDragStart = (event: DragStartEvent) => {
    if (!canReorder) {
      return;
    }

    setActiveDragId(event.active.id);
  };

  const handleDragCancel = () => {
    setActiveDragId(null);
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    setActiveDragId(null);

    const { active, over } = event;
    if (!over || active.id === over.id || reorderInFlight) {
      return;
    }

    const previousEndpoints = endpoints;
    const fromIndex = previousEndpoints.findIndex((endpoint) => endpoint.id === active.id);
    const toIndex = previousEndpoints.findIndex((endpoint) => endpoint.id === over.id);

    if (fromIndex === -1 || toIndex === -1 || fromIndex === toIndex) {
      return;
    }

    const nextEndpoints = arrayMove(previousEndpoints, fromIndex, toIndex);
    setEndpoints(nextEndpoints);
    setSharedEndpoints(revision, nextEndpoints);
    setReorderInFlight(true);

    try {
      const orderedEndpoints = await api.endpoints.movePosition(Number(active.id), toIndex);
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
      setReorderInFlight(false);
    }
  };

  return {
    activeDragEndpoint,
    canReorder,
    collisionDetection: closestCenter,
    endpointIds,
    handleDragCancel,
    handleDragEnd,
    handleDragStart,
    verticalListSortingStrategy,
    reorderInFlight,
    sensors,
  };
}
