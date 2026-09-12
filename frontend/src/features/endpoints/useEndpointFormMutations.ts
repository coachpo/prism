import { useCallback, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import {
  extractEndpointFieldErrors,
  innerDetail,
  isEndpointConfigChangedError,
  isEndpointStaleError,
} from "@/lib/api/endpointErrors";
import type { Endpoint, EndpointVerifyResult } from "@/lib/types";
import { extractServerValidation } from "@/shared/forms/serverValidation";
import type { EndpointReferenceController } from "./useEndpointReferences";
import {
  buildEndpointCreatePayload,
  buildEndpointUpdatePayload,
  type EndpointFormValues,
} from "./endpointSchemas";

export type EndpointVerificationAttempt = {
  result: EndpointVerifyResult | null;
  errorMessage?: string;
  currentEndpoint?: Endpoint;
};

type EndpointFormReferences = Pick<
  EndpointReferenceController,
  "addEndpoint" | "invalidateEndpoint"
>;

type EndpointFormMutationOptions = {
  commitEndpoints: (updater: (current: Endpoint[]) => Endpoint[]) => void;
  references: EndpointFormReferences;
};

export function useEndpointFormMutations({
  commitEndpoints,
  references,
}: EndpointFormMutationOptions) {
  const { addEndpoint, invalidateEndpoint } = references;
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingEndpoint, setEditingEndpointState] =
    useState<Endpoint | null>(null);
  const [endpointDialogError, setEndpointDialogError] = useState<string | null>(
    null,
  );
  const [endpointFieldErrors, setEndpointFieldErrors] = useState<
    Record<string, string> | null
  >(null);

  const replaceEndpoint = useCallback(
    (endpoint: Endpoint) => {
      commitEndpoints((current) =>
        current.map((item) => (item.id === endpoint.id ? endpoint : item)),
      );
    },
    [commitEndpoints],
  );

  const openCreateDialog = useCallback((open: boolean) => {
    if (open) {
      setEndpointDialogError(null);
      setEndpointFieldErrors(null);
    }
    setIsCreateOpen(open);
  }, []);

  const setEditingEndpoint = useCallback((endpoint: Endpoint | null) => {
    if (endpoint) {
      setEndpointDialogError(null);
      setEndpointFieldErrors(null);
    }
    setEditingEndpointState(endpoint);
  }, []);

  const handleVerify = useCallback(
    async (
      endpointId: number,
      family: string,
      expectedRevision: number,
    ): Promise<EndpointVerificationAttempt> => {
      const messages = getStaticMessages();
      try {
        const result = await api.endpoints.verify(endpointId, {
          api_family: family as never,
          expected_config_revision: expectedRevision,
        });
        return { result };
      } catch (error) {
        if (isEndpointConfigChangedError(error)) {
          const changed = innerDetail<{ endpoint: Endpoint }>(error);
          if (changed?.endpoint) replaceEndpoint(changed.endpoint);
          return {
            result: null,
            errorMessage: messages.endpointsUi.verifyResultConfigChanged,
            currentEndpoint: changed?.endpoint,
          };
        }
        return {
          result: null,
          errorMessage: messages.endpointsData.verifyFailed,
        };
      }
    },
    [replaceEndpoint],
  );

  const handleCreate = useCallback(
    async (values: EndpointFormValues, verifyFamily?: string) => {
      const messages = getStaticMessages();
      setEndpointDialogError(null);
      setEndpointFieldErrors(null);
      try {
        const created = await api.endpoints.create(
          buildEndpointCreatePayload(values),
        );
        commitEndpoints((current) => [...current, created]);
        addEndpoint(created.id);
        if (verifyFamily) {
          const verification = await handleVerify(
            created.id,
            verifyFamily,
            created.config_revision,
          );
          return {
            endpoint: created,
            verifyFamily,
            verifyResult: verification.result,
            verifyError: verification.errorMessage,
            currentEndpoint: verification.currentEndpoint,
          };
        }
        return { endpoint: created, verifyFamily };
      } catch (error) {
        const fieldErrors = extractEndpointFieldErrors(error);
        if (fieldErrors) setEndpointFieldErrors(fieldErrors);
        setEndpointDialogError(extractServerValidation(error, messages.endpointsData.createFailed).summary);
        return null;
      }
    },
    [
      commitEndpoints,
      handleVerify,
      addEndpoint,
    ],
  );

  const handleUpdate = useCallback(
    async (values: EndpointFormValues, verifyFamily?: string) => {
      const messages = getStaticMessages();
      if (!editingEndpoint) return null;
      setEndpointDialogError(null);
      setEndpointFieldErrors(null);
      try {
        const updated = await api.endpoints.update(
          editingEndpoint.id,
          buildEndpointUpdatePayload(values, editingEndpoint.updated_at),
        );
        replaceEndpoint(updated);
        invalidateEndpoint(updated.id);
        if (verifyFamily) {
          const verification = await handleVerify(
            updated.id,
            verifyFamily,
            updated.config_revision,
          );
          return {
            endpoint: updated,
            verifyFamily,
            verifyResult: verification.result,
            verifyError: verification.errorMessage,
            currentEndpoint: verification.currentEndpoint,
          };
        }
        return { endpoint: updated, verifyFamily };
      } catch (error) {
        if (isEndpointStaleError(error)) {
          const stale = innerDetail<{ endpoint: Endpoint }>(error);
          const current = stale?.endpoint;
          if (current) {
            replaceEndpoint(current);
          }
          setEndpointDialogError(messages.endpointsData.endpointStale);
          return null;
        }
        const fieldErrors = extractEndpointFieldErrors(error);
        if (fieldErrors) setEndpointFieldErrors(fieldErrors);
        setEndpointDialogError(extractServerValidation(error, messages.endpointsData.updateFailed).summary);
        return null;
      }
    },
    [
      editingEndpoint,
      handleVerify,
      invalidateEndpoint,
      replaceEndpoint,
    ],
  );

  return {
    endpointDialogError,
    endpointFieldErrors,
    editingEndpoint,
    handleCreate,
    handleUpdate,
    handleVerify,
    isCreateOpen,
    setEditingEndpoint,
    setIsCreateOpen: openCreateDialog,
  };
}
