package runtime

import (
	"net/http"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

func NewRuntimeTypedHookPayloadInput(request *http.Request, operationMatch RuntimeOperationMatch, rawBody []byte, phase gatewaycore.HookPhase) gatewaycore.HookPayloadInput {
	envelope := newGatewayCoreRequestEnvelope(request, operationMatch)
	envelope.Body = append([]byte(nil), rawBody...)
	return gatewaycore.HookPayloadInput{
		Phase:    phase,
		Envelope: envelope,
		AdditionalMetadata: map[string]string{
			"hook_collection_id": operationMatch.Operation.HookCollectionID,
		},
	}
}
