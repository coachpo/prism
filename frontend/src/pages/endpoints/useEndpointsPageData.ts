import { useMemo, useState } from "react";
import { toast } from "sonner";
import { getStaticMessages } from "@/i18n/staticMessages";
import { useTimezone } from "@/hooks/useTimezone";
import { api } from "@/lib/api";
import type { Endpoint } from "@/lib/types";
import { useProfileContext } from "@/context/ProfileContext";
import type { EndpointFormValues } from "./EndpointDialog";
import { useEndpointBootstrapData } from "./useEndpointBootstrapData";
import { useEndpointReorder } from "./useEndpointReorder";

type ReviewFilter = "all" | "in-use" | "unused";

export function useEndpointsPageData() {
  const [isDeletingEndpoint, setIsDeletingEndpoint] = useState(false);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState<Endpoint | null>(null);
  const [duplicatingEndpointId, setDuplicatingEndpointId] = useState<number | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Endpoint | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>("all");
  const { revision } = useProfileContext();
  const { format: formatTime } = useTimezone();
  const { commitEndpoints, endpointModels, endpoints, isLoading, setEndpoints } =
    useEndpointBootstrapData(revision);

  const normalizedSearch = searchQuery.trim().toLowerCase();
  const hasActiveReviewFilters = normalizedSearch.length > 0 || reviewFilter !== "all";

  const filteredEndpoints = useMemo(() => {
    return endpoints.filter((endpoint) => {
      const models = endpointModels[endpoint.id] ?? [];
      const matchesSearch =
        normalizedSearch.length === 0 ||
        endpoint.name.toLowerCase().includes(normalizedSearch) ||
        endpoint.base_url.toLowerCase().includes(normalizedSearch);
      const matchesUsage =
        reviewFilter === "all" ||
        (reviewFilter === "in-use" ? models.length > 0 : models.length === 0);

      return matchesSearch && matchesUsage;
    });
  }, [endpointModels, endpoints, normalizedSearch, reviewFilter]);

  const visibleEndpointIds = useMemo(
    () => filteredEndpoints.map((endpoint) => endpoint.id),
    [filteredEndpoints],
  );

  const reorder = useEndpointReorder({
    endpoints,
    revision,
    setEndpoints,
    filtersActive: hasActiveReviewFilters,
  });

  const handleCreate = async (values: EndpointFormValues) => {
    const messages = getStaticMessages();
    try {
      const created = await api.endpoints.create(values);
      toast.success(messages.endpointsData.created);
      setIsCreateOpen(false);
      commitEndpoints(
        (current) => [...current, created].sort((left, right) => left.position - right.position),
        (current) => ({ ...current, [created.id]: [] }),
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.createFailed);
    }
  };

  const handleUpdate = async (values: EndpointFormValues) => {
    const messages = getStaticMessages();
    if (!editingEndpoint) {
      return;
    }

    try {
      const updated = await api.endpoints.update(editingEndpoint.id, {
        name: values.name,
        base_url: values.base_url,
        ...(values.api_key.trim() ? { api_key: values.api_key } : {}),
      });
      toast.success(messages.endpointsData.updated);
      setEditingEndpoint(null);
      commitEndpoints((current) =>
        current.map((endpoint) => (endpoint.id === updated.id ? updated : endpoint)),
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.updateFailed);
    }
  };

  const handleDelete = async (id: number) => {
    const messages = getStaticMessages();
    setIsDeletingEndpoint(true);
    try {
      await api.endpoints.delete(id);
      toast.success(messages.endpointsData.deleted);
      setDeleteTarget(null);
      commitEndpoints(
        (current) => current.filter((endpoint) => endpoint.id !== id),
        (current) => {
          const next = { ...current };
          delete next[id];
          return next;
        },
      );
    } catch (error) {
      if (error instanceof Error) {
        const normalizedMessage = error.message.toLowerCase();
        if (
          normalizedMessage.includes("dependency") ||
          normalizedMessage.includes("409") ||
          normalizedMessage.includes("cannot delete endpoint") ||
          normalizedMessage.includes("referenced by connections")
        ) {
          toast.error(error.message);
          return;
        }
      }
      toast.error(error instanceof Error ? error.message : messages.endpointsData.deleteFailed);
    } finally {
      setIsDeletingEndpoint(false);
    }
  };

  const handleDuplicateEndpoint = async (endpoint: Endpoint) => {
    const messages = getStaticMessages();
    setDuplicatingEndpointId(endpoint.id);
    try {
      const duplicate = await api.endpoints.duplicate(endpoint.id);
      toast.success(messages.endpointsData.duplicatedAs(duplicate.name));
      commitEndpoints(
        (current) => [...current, duplicate].sort((left, right) => left.position - right.position),
        (current) => ({ ...current, [duplicate.id]: [] }),
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.endpointsData.duplicateFailed);
    } finally {
      setDuplicatingEndpointId(null);
    }
  };

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (!open && !isDeletingEndpoint) {
      setDeleteTarget(null);
    }
  };

  return {
    deleteTarget,
    duplicatingEndpointId,
    editingEndpoint,
    endpointModels,
    endpoints,
    filteredEndpoints,
    formatTime,
    hasActiveReviewFilters,
    handleCreate,
    handleDelete,
    handleDeleteDialogOpenChange,
    handleDuplicateEndpoint,
    handleUpdate,
    isCreateOpen,
    isDeletingEndpoint,
    isLoading,
    reviewFilter,
    searchQuery,
    setDeleteTarget,
    setEditingEndpoint,
    setIsCreateOpen,
    setReviewFilter,
    setSearchQuery,
    ...reorder,
    visibleEndpointIds,
  };
}
