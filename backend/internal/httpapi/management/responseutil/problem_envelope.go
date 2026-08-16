package responseutil

import (
	"crypto/rand"
	"encoding/hex"

	"net/http"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// problemEnvelopeRequestIDBytes is the length of the hex-encoded opaque
// request correlation id included in flat management problem envelopes.
const problemEnvelopeRequestIDBytes = 8

// WriteProblem writes the canonical flat management error envelope
// {code, detail, params, details, request_id} that the umbrella management
// problem registry owns. Auth is the first consumer; other management
// surfaces converge on the same writer instead of replicating JSON shapes.
//
// params is the exact wire params object (usually an empty map) and details
// is the registered typed details payload or nil for empty details. The
// request_id is a fresh opaque, non-sensitive correlation value generated
// per write.
func WriteProblem(
	w http.ResponseWriter,
	r *http.Request,
	corsSnapshot platformcors.Snapshot,
	statusCode int,
	code string,
	detail string,
	params map[string]any,
	details any,
) {
	applyAllowedOrigin(w, r, corsSnapshot)
	payload := map[string]any{
		"code":       code,
		"detail":     detail,
		"params":     params,
		"details":    details,
		"request_id": newProblemRequestID(),
	}
	WriteJSON(w, statusCode, payload)
}

func newProblemRequestID() string {
	buf := make([]byte, problemEnvelopeRequestIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf)
}
