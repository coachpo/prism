package runtime

// Model rewrite owns the narrow provider-path/body identity adjustment after
// routing has selected a final target. Body rewrites preserve the existing
// payload shape while changing only the provider model member; path rewrites
// replace the exact Gemini model segment.
//
// These functions are deliberately best-effort value transforms. Operation
// binding decides whether a model is required, while planning decides whether
// a rewrite is allowed for the selected operation.
//
// No provider response rewriting belongs here.
//
// JSON decode failures return the original body so the caller can preserve its
// existing malformed-body boundary. A successful rewrite re-encodes the body
// for the selected upstream attempt only; the client-facing response remains
// native to the provider operation.
//
// Path replacement is limited to the registered `/models/{model}` segment and
// does not perform a broad string substitution over query values or payload
// text. The operation registry supplies the original path model identity.
//
// These rules keep requested model observability separate from resolved target
// model identity.
//
// The selected Terminal Target owns the final upstream body and path. This
// module does not mutate a planning snapshot, a shared map, or a response
// envelope. Every returned byte slice is owned by the caller's attempt.
//
// Model rewrite is therefore a narrow value boundary with no retry policy.
// A failed rewrite keeps the original input so the caller retains its normal
// error classification boundary.
//
//
// The rewrite boundary is deliberately provider-neutral.
//
// Provider adapters decide the native body before this value transform.
// The planner controls when a transform is required.
// No retry or admission state is consulted.
// The function is deterministic for one selected attempt.
//
import (
	"encoding/json"
	"strings"
)

func extractModelFromBody(rawBody []byte) string {
	if len(rawBody) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return ""
	}
	modelID, _ := payload["model"].(string)
	return strings.TrimSpace(modelID)
}

func rewriteModelInBody(rawBody []byte, targetModelID string) []byte {
	if len(rawBody) == 0 {
		return rawBody
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return rawBody
	}
	payload["model"] = targetModelID
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return rawBody
	}
	return rewritten
}

func rewriteModelInPath(requestPath string, originalModel string, targetModel string) string {
	if originalModel == targetModel {
		return requestPath
	}
	return strings.Replace(requestPath, "/models/"+originalModel, "/models/"+targetModel, 1)
}
