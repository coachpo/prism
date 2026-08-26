import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  EndpointCreate,
} from "@/lib/types";
import type { ConnectionDialogForm, HeaderRow } from "./useModelDetailDialogState";
import {
  parseCustomRequestParametersDraft,
  type CustomRequestParametersParseError,
} from "./customRequestParameters";
import {
  parseRoutingScheduleDraft,
  type RoutingScheduleDraft,
  type RoutingScheduleDraftError,
} from "./routingScheduleDraft";
import { buildConnectionDraftPayload } from "./connectionDataSupport";

export interface ConnectionSubmitPreparationInput {
  apiFamily: ApiFamily | null;
  createMode: "select" | "new";
  selectedEndpointId: string;
  newEndpointForm: EndpointCreate;
  connectionForm: ConnectionDialogForm;
  headerRows: HeaderRow[];
  customRequestParametersDraft: string;
  routingScheduleDraft: RoutingScheduleDraft;
  editingConnection: Connection | null;
  endpointSourceDefaultName: string | null;
}

export type ConnectionSubmitPreparation =
  | {
      kind: "custom_request_parameters_error";
      error: CustomRequestParametersParseError;
    }
  | {
      kind: "routing_schedule_error";
      error: RoutingScheduleDraftError;
    }
  | {
      kind: "payload_error";
      errorMessage: string | null;
    }
  | { kind: "ready"; payload: ConnectionCreate };

/**
 * Parses the two raw editor drafts and shapes the single wire payload before
 * any create/update request. No toast or state mutation belongs in this
 * boundary; the mutation owner maps the result to its field state.
 */
export function prepareConnectionSubmit(
  input: ConnectionSubmitPreparationInput,
): ConnectionSubmitPreparation {
  const parsedCustomRequestParameters = parseCustomRequestParametersDraft(
    input.customRequestParametersDraft,
  );
  if (parsedCustomRequestParameters.error) {
    return {
      kind: "custom_request_parameters_error",
      error: parsedCustomRequestParameters.error,
    };
  }

  const parsedRoutingSchedule = parseRoutingScheduleDraft(
    input.routingScheduleDraft,
  );
  if (parsedRoutingSchedule.error) {
    return {
      kind: "routing_schedule_error",
      error: parsedRoutingSchedule.error,
    };
  }

  const { errorMessage, payload } = buildConnectionDraftPayload({
    apiFamily: input.apiFamily,
    createMode: input.createMode,
    selectedEndpointId: input.selectedEndpointId,
    newEndpointForm: input.newEndpointForm,
    connectionForm: input.connectionForm,
    headerRows: input.headerRows,
    customRequestParametersValue: parsedCustomRequestParameters.value,
    routingScheduleValue: parsedRoutingSchedule.value,
    editingConnection: input.editingConnection,
    endpointSourceDefaultName: input.endpointSourceDefaultName,
  });

  if (!payload) return { kind: "payload_error", errorMessage };
  return { kind: "ready", payload };
}
