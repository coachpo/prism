package contracttest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func assertStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		body := readResponseBody(t, response)
		t.Fatalf("expected status %d, got %d with body %s", want, response.StatusCode, body)
	}
}

func assertErrorResponse(t *testing.T, response *http.Response, wantStatus int, wantDetail string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail, ok := payload["detail"].(string)
	if !ok {
		t.Fatalf("expected error detail string, got %+v", payload)
	}
	if detail != wantDetail {
		t.Fatalf("expected error detail %q, got %+v", wantDetail, payload)
	}
}

func assertErrorResponseCode(t *testing.T, response *http.Response, wantStatus int, wantCode string, wantDetail string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]string
	decodeJSONResponse(t, response, &payload)
	if payload["code"] != wantCode || payload["detail"] != wantDetail {
		t.Fatalf("expected error code/detail %q/%q, got %+v", wantCode, wantDetail, payload)
	}
}

// assertAuthProblemResponse asserts the flat management problem envelope for
// registered auth codes: exact status, exact code, exact empty params and a
// request_id present.
func assertAuthProblemResponse(t *testing.T, response *http.Response, wantStatus int, wantCode string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["code"] != wantCode {
		t.Fatalf("expected auth problem code %q, got %+v", wantCode, payload)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok || len(params) != 0 {
		t.Fatalf("expected auth problem params to be exact empty object, got %+v", payload["params"])
	}
	requestID, ok := payload["request_id"].(string)
	if !ok || requestID == "" {
		t.Fatalf("expected auth problem request_id, got %+v", payload)
	}
}

// assertLoginLockedProblem asserts the auth_login_locked envelope: Retry-After
// header and details.retry_at/retry_after_seconds must be present and the
// header delta must equal the body delta.
func assertLoginLockedProblem(t *testing.T, response *http.Response) {
	t.Helper()
	assertAuthProblemResponse(t, response, http.StatusTooManyRequests, "auth_login_locked")
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	details, ok := payload["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected auth_login_locked details object, got %+v", payload["details"])
	}
	retryAt, ok := details["retry_at"].(string)
	if !ok || retryAt == "" {
		t.Fatalf("expected auth_login_locked details.retry_at, got %+v", details)
	}
	retryAfter, ok := details["retry_after_seconds"].(float64)
	if !ok || retryAfter < 0 {
		t.Fatalf("expected auth_login_locked details.retry_after_seconds, got %+v", details)
	}
	headerValue := response.Header.Get("Retry-After")
	if headerValue == "" {
		t.Fatalf("expected Retry-After header on auth_login_locked, headers=%v", response.Header)
	}
	parsed, err := strconv.ParseInt(headerValue, 10, 64)
	if err != nil || int64(retryAfter) != parsed {
		t.Fatalf("expected Retry-After header to equal details.retry_after_seconds (%v), got %q", retryAfter, headerValue)
	}
}

func assertSessionPayload(t *testing.T, response *http.Response, authenticated bool, authEnabled bool, username *string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["authenticated"] != authenticated || payload["auth_enabled"] != authEnabled {
		t.Fatalf("expected session payload authenticated=%v auth_enabled=%v, got %+v", authenticated, authEnabled, payload)
	}
	if username == nil {
		if payload["username"] != nil {
			t.Fatalf("expected null username, got %+v", payload)
		}
		return
	}
	if payload["username"] != *username {
		t.Fatalf("expected username %q, got %+v", *username, payload)
	}
}

func assertSuccessPayload(t *testing.T, response *http.Response) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["success"] != true {
		t.Fatalf("expected success payload, got %+v", payload)
	}
}

func assertCookiePresent(t *testing.T, response *http.Response, name string) {
	t.Helper()
	_ = responseCookie(t, response, name)
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("expected response to set cookie %q", name)
	return nil
}

func assertNoResponseCookie(t *testing.T, response *http.Response, name string) {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			t.Fatalf("expected response not to set cookie %q", name)
		}
	}
}

func decodeAccessTokenClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT token with 3 parts, got %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	return claims
}

func assertJWTSignature(t *testing.T, token string, secret string, wantValid bool) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT token with 3 parts, got %q", token)
	}
	signer := hmac.New(sha256.New, []byte(secret))
	_, _ = signer.Write([]byte(parts[0] + "." + parts[1]))
	got := hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(signer.Sum(nil))), []byte(parts[2]))
	if got != wantValid {
		t.Fatalf("expected JWT signature validity for secret %q to be %v, got %v", secret, wantValid, got)
	}
}

func cookieValue(t *testing.T, client *http.Client, rawURL string, name string) string {
	t.Helper()
	urlValue, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build cookie request: %v", err)
	}
	for _, cookie := range client.Jar.Cookies(urlValue.URL) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func decodeJSONResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	body := readResponseBody(t, response)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode response JSON %q: %v", body, err)
	}
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := ioReadAll(response)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	response.Body = ioNopCloser(bytes.NewReader(raw))
	return strings.TrimSpace(string(raw))
}

func ioReadAll(response *http.Response) ([]byte, error) {
	return io.ReadAll(response.Body)
}

func ioNopCloser(reader *bytes.Reader) io.ReadCloser {
	return io.NopCloser(reader)
}
