import { useCallback, useState } from "react";
import { toast } from "sonner";

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
  onEndpointCreated: (endpoint: Endpoint) => void;
};

export function useEndpointFormMutations({
  commitEndpoints,
  references,
  onEndpointCreated,
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
      try {
        const created = await api.endpoints.create(
          buildEndpointCreatePayload(values),
        );
        toast.success(messages.endpointsData.created);
        if (!verifyFamily) setIsCreateOpen(false);
        commitEndpoints((current) => [...current, created]);
        addEndpoint(created.id);
        onEndpointCreated(created);
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
        const validation = extractServerValidation(
          error,
          messages.endpointsData.createFailed,
        );
        setEndpointDialogError(validation.summary);
        toast.error(validation.summary);
        return null;
      }
    },
    [
      commitEndpoints,
      handleVerify,
      onEndpointCreated,
      addEndpoint,
    ],
  );

  const handleUpdate = useCallback(
    async (values: EndpointFormValues, verifyFamily?: string) => {
      const messages = getStaticMessages();
      if (!editingEndpoint) return null;
      try {
        const updated = await api.endpoints.update(
          editingEndpoint.id,
          buildEndpointUpdatePayload(values, editingEndpoint.updated_at),
        );
        const keyRotated =
          updated.api_key_updated_at !== editingEndpoint.api_key_updated_at;
        toast.success(
          keyRotated && updated.api_key_fingerprint
            ? messages.endpointsData.keyRotated(updated.api_key_fingerprint)
            : messages.endpointsData.keyUnchanged,
        );
        if (!verifyFamily) setEditingEndpoint(null);
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
            setEditingEndpoint(current);
          }
          setEndpointDialogError(messages.endpointsData.endpointStale);
          toast.error(messages.endpointsData.endpointStale);
          return null;
        }
        const fieldErrors = extractEndpointFieldErrors(error);
        if (fieldErrors) setEndpointFieldErrors(fieldErrors);
        const validation = extractServerValidation(
          error,
          messages.endpointsData.updateFailed,
        );
        setEndpointDialogError(validation.summary);
        toast.error(validation.summary);
        return null;
      }
    },
    [
      editingEndpoint,
      handleVerify,
      invalidateEndpoint,
      replaceEndpoint,
      setEditingEndpoint,
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
