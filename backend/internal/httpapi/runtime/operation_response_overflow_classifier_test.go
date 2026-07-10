package runtime

import (
	"net/http"
	"testing"
)

func TestOverflowClassifierAcceptsContextLengthExceeded(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "native 400", status: http.StatusBadRequest},
		{name: "native 413", status: http.StatusRequestEntityTooLarge},
		{name: "native 422", status: http.StatusUnprocessableEntity},
	}
	payload := `{"error":{"message":"This request exceeded the maximum context length for the selected model.","type":"invalid_request_error","code":"context_length_exceeded"}}`
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertOverflowClassifierAccepted(t, test.status, payload, cliProxyAPIOverflowClassifierErrorCode, "context_length_exceeded")
		})
	}
}

func TestOverflowClassifierAcceptsBodyConfirmed429Overflow(t *testing.T) {
	t.Run("native flat gateway code", func(t *testing.T) {
		payload := `{"code":"context_too_large","detail":"The request exceeded the maximum context length for this model."}`
		assertOverflowClassifierAccepted(t, http.StatusTooManyRequests, payload, cliProxyAPIOverflowClassifierErrorCode, "context_too_large")
	})
	t.Run("native openai message fallback", func(t *testing.T) {
		payload := `{"error":{"message":"The request exceeded the context window and is too long for this model.","type":"invalid_request_error"}}`
		assertOverflowClassifierAccepted(t, http.StatusTooManyRequests, payload, cliProxyAPIOverflowClassifierMessageText, "")
	})
}

func TestOverflowClassifierRejectsPlain429(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusTooManyRequests, `{"error":{"message":"Request rejected by upstream.","type":"server_error"}}`)
}

func TestOverflowClassifierRejectsQuota429(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusTooManyRequests, `{"error":{"message":"Insufficient quota for this request.","code":"insufficient_quota"}}`)
}

func TestOverflowClassifierRejectsRateLimit429(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusTooManyRequests, `{"error":{"message":"Rate limit exceeded for tokens per minute.","code":"rate_limit_exceeded"}}`)
}

func TestOverflowClassifierRejectsCapacity429(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusTooManyRequests, `{"message":"Server overloaded, try again later."}`)
}

func TestOverflowClassifierRejectsAuthFailure(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusBadRequest, `{"error":{"message":"Incorrect API key provided.","code":"invalid_api_key"}}`)
}

func TestOverflowClassifierRejectsMalformedJSON(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusBadRequest, `{"error":{"message":"broken"}`)
}

func TestOverflowClassifierRejectsAmbiguousBody(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusBadRequest, `{"error":{"message":"The request is too large."}}`)
}

func TestOverflowClassifierRejectsModelLookupFailure(t *testing.T) {
	assertOverflowClassifierRejected(t, http.StatusBadRequest, `{"error":{"message":"Model not found.","code":"model_not_found"}}`)
}

func assertOverflowClassifierAccepted(t *testing.T, statusCode int, payload string, wantClassifier string, wantErrorCode string) {
	t.Helper()
	classification := classifyCLIProxyAPIOverflowResponse(statusCode, []byte(payload))
	if !classification.Promotable {
		t.Fatalf("expected promotable classification, got %+v", classification)
	}
	if classification.Classifier != wantClassifier || classification.ErrorCode != wantErrorCode {
		t.Fatalf("expected classifier=%q errorCode=%q, got %+v", wantClassifier, wantErrorCode, classification)
	}
}

func assertOverflowClassifierRejected(t *testing.T, statusCode int, payload string) {
	t.Helper()
	classification := classifyCLIProxyAPIOverflowResponse(statusCode, []byte(payload))
	if classification.Promotable {
		t.Fatalf("expected non-promotable classification, got %+v", classification)
	}
}
