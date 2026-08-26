import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import {
  innerDetail,
  isEndpointInUseError,
  isReferenceIntegrityError,
} from "@/lib/api/endpointErrors";
import { ApiError } from "@/lib/api/request";
import type {
  Endpoint,
  EndpointReferenceDetail,
  EndpointReferencePage,
  EndpointReferenceSummary,
} from "@/lib/types";
import type { EndpointReferenceController } from "./useEndpointReferences";

export type DeleteDialogState =
  | { phase: "closed" }
  | { phase: "checking"; endpoint: Endpoint; generation: number }
  | {
      phase: "eligible";
      endpoint: Endpoint;
      summary: EndpointReferenceSummary;
      generation: number;
    }
  | {
      phase: "blocked";
      endpoint: Endpoint;
      detail: EndpointReferenceDetail;
      generation: number;
    }
  | {
      phase: "check_error";
      endpoint: Endpoint;
      error: ApiError;
      generation: number;
    }
  | {
      phase: "integrity_error";
      endpoint: Endpoint;
      error: ApiError;
      generation: number;
    }
  | { phase: "deleting"; endpoint: Endpoint; generation: number };

type EndpointDeletionReferences = Pick<
  EndpointReferenceController,
  "adoptDetail" | "loadMore" | "removeEndpoint"
>;

type EndpointDeletionOptions = {
  endpoints: Endpoint[];
  commitEndpoints: (updater: (current: Endpoint[]) => Endpoint[]) => void;
  references: EndpointDeletionReferences;
};

export function useEndpointDeletion({
  endpoints,
  commitEndpoints,
  references,
}: EndpointDeletionOptions) {
  const { adoptDetail, loadMore, removeEndpoint } = references;
  const [deleteDialog, setDeleteDialog] = useState<DeleteDialogState>({
    phase: "closed",
  });
  const deleteGeneration = useRef(0);

  const issueDeleteGeneration = useCallback(() => {
    deleteGeneration.current += 1;
    return deleteGeneration.current;
  }, []);

  const isCurrentDelete = useCallback(
    (generation: number) => deleteGeneration.current === generation,
    [],
  );

  const handleDeleteRequest = useCallback(
    (endpoint: Endpoint) => {
      // Every dialog open runs a fresh single-reference preflight.
      const requestGeneration = issueDeleteGeneration();
      setDeleteDialog({
        phase: "checking",
        endpoint,
        generation: requestGeneration,
      });
      void (async () => {
        try {
          const detail = await api.endpoints.referencesDetail(endpoint.id);
          if (!isCurrentDelete(requestGeneration)) return;
          adoptDetail(endpoint.id, detail);
          setDeleteDialog((current) => {
            if (
              current.phase !== "checking" ||
              current.endpoint.id !== endpoint.id ||
              current.generation !== requestGeneration
            ) {
              return current;
            }
            if (detail.summary.direct_reference_count === 0) {
              return {
                phase: "eligible",
                endpoint,
                summary: detail.summary,
                generation: requestGeneration,
              };
            }
            return {
              phase: "blocked",
              endpoint,
              detail,
              generation: requestGeneration,
            };
          });
        } catch (error) {
          if (!isCurrentDelete(requestGeneration)) return;
          const apiError =
            error instanceof ApiError
              ? error
              : new ApiError(
                  error instanceof Error
                    ? error.message
                    : "Failed to check references",
                  0,
                  null,
                );
          setDeleteDialog((current) => {
            if (
              current.phase !== "checking" ||
              current.endpoint.id !== endpoint.id ||
              current.generation !== requestGeneration
            ) {
              return current;
            }
            if (isReferenceIntegrityError(error)) {
              return {
                phase: "integrity_error",
                endpoint,
                error: apiError,
                generation: requestGeneration,
              };
            }
            return {
              phase: "check_error",
              endpoint,
              error: apiError,
              generation: requestGeneration,
            };
          });
        }
      })();
    },
    [adoptDetail, isCurrentDelete, issueDeleteGeneration],
  );

  const handleDeleteConfirm = useCallback(
    async (target: { id: number }) => {
      const current = deleteDialog;
      const endpoint =
        current.phase !== "closed" && current.endpoint.id === target.id
          ? current.endpoint
          : endpoints.find((item) => item.id === target.id);
      const requestGeneration =
        current.phase !== "closed" && current.endpoint.id === target.id
          ? current.generation
          : null;
      if (!endpoint || requestGeneration == null) return;
      const messages = getStaticMessages();
      setDeleteDialog((currentState) => {
        if (
          currentState.phase !== "eligible" ||
          currentState.endpoint.id !== endpoint.id ||
          currentState.generation !== requestGeneration
        ) {
          return currentState;
        }
        return { ...currentState, phase: "deleting" };
      });
      try {
        await api.endpoints.delete(endpoint.id);
        if (!isCurrentDelete(requestGeneration)) return;
        const currentState = deleteDialog;
        if (
          currentState.phase !== "eligible" &&
          currentState.phase !== "deleting"
        ) {
          return;
        }
        toast.success(messages.endpointsData.deleted);
        setDeleteDialog({ phase: "closed" });
        removeEndpoint(endpoint.id);
        commitEndpoints((currentItems) =>
          currentItems.filter((item) => item.id !== endpoint.id),
        );
      } catch (error) {
        if (!isCurrentDelete(requestGeneration)) return;
        if (isEndpointInUseError(error)) {
          // Race: a reference appeared after preflight. Replace the dialog
          // with the response's latest summary + bounded first page.
          const race = innerDetail<{
            endpoint_id: number;
            summary: EndpointReferenceSummary;
            reference_page: EndpointReferencePage;
          }>(error);
          if (!race) return;
          const detail: EndpointReferenceDetail = {
            endpoint_id: race.endpoint_id,
            summary: race.summary,
            reference_page: race.reference_page,
          };
          adoptDetail(endpoint.id, detail);
          setDeleteDialog((currentState) => {
            if (
              currentState.phase !== "deleting" ||
              currentState.endpoint.id !== endpoint.id ||
              currentState.generation !== requestGeneration
            ) {
              return currentState;
            }
            return {
              phase: "blocked",
              endpoint,
              detail,
              generation: requestGeneration,
            };
          });
          return;
        }
        if (isReferenceIntegrityError(error)) {
          setDeleteDialog((currentState) => {
            if (
              currentState.phase !== "deleting" ||
              currentState.endpoint.id !== endpoint.id ||
              currentState.generation !== requestGeneration
            ) {
              return currentState;
            }
            return {
              phase: "integrity_error",
              endpoint,
              error:
                error instanceof ApiError
                  ? error
                  : new ApiError("Integrity error", 409, null),
              generation: requestGeneration,
            };
          });
          return;
        }
        setDeleteDialog((currentState) => {
          if (
            currentState.phase !== "deleting" ||
            currentState.endpoint.id !== endpoint.id ||
            currentState.generation !== requestGeneration
          ) {
            return currentState;
          }
          return {
            phase: "check_error",
            endpoint,
            error:
              error instanceof ApiError
                ? error
                : new ApiError(
                    error instanceof Error
                      ? error.message
                      : messages.endpointsData.deleteFailed,
                    0,
                    null,
                  ),
            generation: requestGeneration,
          };
        });
      }
    },
    [
      commitEndpoints,
      deleteDialog,
      endpoints,
      isCurrentDelete,
      adoptDetail,
      removeEndpoint,
    ],
  );

  const handleDeleteDialogOpenChange = useCallback(
    (open: boolean) => {
      if (!open && deleteDialog.phase !== "deleting") {
        issueDeleteGeneration();
        setDeleteDialog({ phase: "closed" });
      }
    },
    [deleteDialog.phase, issueDeleteGeneration],
  );

  const handleDeleteRetry = useCallback(() => {
    if (deleteDialog.phase === "closed") return;
    const endpoint = deleteDialog.endpoint;
    setDeleteDialog({ phase: "closed" });
    handleDeleteRequest(endpoint);
  }, [deleteDialog, handleDeleteRequest]);

  const handleLoadMoreBlockers = useCallback(
    async (endpointId: number) => {
      const current = deleteDialog;
      if (
        current.phase !== "blocked" ||
        current.endpoint.id !== endpointId
      ) {
        return;
      }
      const requestGeneration = current.generation;
      const detail = await loadMore(endpointId);
      if (!detail) return;
      setDeleteDialog((next) => {
        if (
          next.phase !== "blocked" ||
          next.endpoint.id !== endpointId ||
          next.generation !== requestGeneration
        ) {
          return next;
        }
        return { ...next, detail };
      });
    },
    [deleteDialog, loadMore],
  );

  return {
    deleteDialog,
    handleDeleteConfirm,
    handleDeleteDialogOpenChange,
    handleDeleteRequest,
    handleDeleteRetry,
    handleLoadMoreBlockers,
  };
}
