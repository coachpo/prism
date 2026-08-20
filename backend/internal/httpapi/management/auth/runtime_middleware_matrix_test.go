package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

// fakeRuntimeAuthCache substitutes the runtime shared cache for middleware
// decision-matrix tests. It records every raw key handed to verification so
// tests can assert extraction priority and no-fallback-after-selection.
type fakeRuntimeAuthCache struct {
	mu          sync.Mutex
	authEnabled bool
	authErr     error
	decision    RuntimeProxyKeyDecision
	verifyErr   error
	verified    []string
}

func (f *fakeRuntimeAuthCache) LoadFreshRuntimeAuthSettings(ctx context.Context) (RuntimeAuthSettingsSnapshot, error) {
	return RuntimeAuthSettingsSnapshot{AuthEnabled: f.authEnabled}, f.authErr
}

func (f *fakeRuntimeAuthCache) LoadFreshRuntimeProxyKeyDecision(ctx context.Context, now time.Time, rawKey string) (RuntimeProxyKeyDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified = append(f.verified, rawKey)
	if f.verifyErr != nil {
		return RuntimeProxyKeyDecision{}, f.verifyErr
	}
	return f.decision, nil
}

func (f *fakeRuntimeAuthCache) Invalidate() {}

func (f *fakeRuntimeAuthCache) verifiedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.verified...)
}

type matrixCapture struct {
	attribution    requestcontext.RuntimeProxyKeyAttribution
	hasAttribution bool
}

func newMatrixService(cache runtimeAuthCache) *Service {
	return &Service{
		now:                func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) },
		runtimeCache:       cache,
		corsOriginProvider: platformcors.NewStaticOriginProvider(nil),
	}
}

func runMatrixRequest(t *testing.T, service *Service, headers map[string]string) (int, string, matrixCapture) {
	t.Helper()
	capture := matrixCapture{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attribution, ok := requestcontext.RuntimeProxyKeyAttributionFromContext(r.Context()); ok {
			capture.attribution = attribution
			capture.hasAttribution = true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	handler := service.RuntimeMiddleware(next)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String(), capture
}

func matrixFixtureCredential(suffix string) string {
	return "pm" + "-" + suffix
}

func TestRuntimeMiddlewareDecisionMatrix(t *testing.T) {
	validDecision := RuntimeProxyKeyDecision{Allowed: true, KeyID: 42, KeyName: "production-client"}
	validCredential := matrixFixtureCredential("1234567890abcdef1234567890abcdef")

	tests := []struct {
		name         string
		authEnabled  bool
		authErr      error
		decision     RuntimeProxyKeyDecision
		verifyErr    error
		headers      map[string]string
		wantStatus   int
		wantState    requestcontext.RuntimeProxyKeyAttributionState
		wantEnforce  bool
		wantID       int
		wantName     string
		wantVerified []string
	}{
		{
			name: "enabled absent credential", authEnabled: true, headers: map[string]string{},
			wantStatus: http.StatusUnauthorized, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name: "enabled valid bearer", authEnabled: true, decision: validDecision,
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyIdentified,
			wantEnforce: true, wantID: 42, wantName: "production-client",
			wantVerified: []string{validCredential},
		},
		{
			name: "enabled unknown credential", authEnabled: true, headers: map[string]string{"Authorization": "Bearer " + matrixFixtureCredential("ffffffffffffffffffffffffffffffff")},
			wantStatus: http.StatusUnauthorized, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name: "enabled inactive credential", authEnabled: true, decision: RuntimeProxyKeyDecision{},
			headers:    map[string]string{"X-API-Key": matrixFixtureCredential("11111111111111111111111111111111")},
			wantStatus: http.StatusUnauthorized, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name: "enabled expired credential", authEnabled: true, decision: RuntimeProxyKeyDecision{},
			headers:    map[string]string{"X-Goog-Api-Key": matrixFixtureCredential("22222222222222222222222222222222")},
			wantStatus: http.StatusUnauthorized, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:        "enabled malformed authorization falls through to lower priority",
			authEnabled: true, decision: validDecision,
			headers:    map[string]string{"Authorization": "Basic abc", "X-API-Key": validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyIdentified,
			wantEnforce: true, wantID: 42, wantName: "production-client",
			wantVerified: []string{validCredential},
		},
		{
			name:        "enabled valid high priority never falls back after selection",
			authEnabled: true, decision: RuntimeProxyKeyDecision{},
			headers:    map[string]string{"Authorization": "Bearer " + matrixFixtureCredential("33333333333333333333333333333333"), "X-API-Key": validCredential},
			wantStatus: http.StatusUnauthorized, wantState: requestcontext.RuntimeProxyKeyNone,
			wantVerified: []string{matrixFixtureCredential("33333333333333333333333333333333")},
		},
		{
			name:        "enabled verifier unavailable fails closed 503",
			authEnabled: true, verifyErr: runtimeSnapshotUnavailableError(),
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusServiceUnavailable, wantState: requestcontext.RuntimeProxyKeyNone,
			wantVerified: []string{validCredential},
		},
		{
			name:        "enabled generic verifier failure fails closed 503",
			authEnabled: true, verifyErr: context.DeadlineExceeded,
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusServiceUnavailable, wantState: requestcontext.RuntimeProxyKeyNone,
			wantVerified: []string{validCredential},
		},
		{
			name: "enabled auth snapshot unavailable 503", authEnabled: false, authErr: runtimeSnapshotUnavailableError(),
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusServiceUnavailable, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:       "disabled absent credential continues none",
			headers:    map[string]string{},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:       "disabled valid bearer identified without enforcement",
			decision:   validDecision,
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyIdentified,
			wantEnforce: false, wantID: 42, wantName: "production-client",
			wantVerified: []string{validCredential},
		},
		{
			name:       "disabled unknown credential continues none",
			headers:    map[string]string{"Authorization": "Bearer " + matrixFixtureCredential("44444444444444444444444444444444")},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:       "disabled inactive credential continues none",
			decision:   RuntimeProxyKeyDecision{},
			headers:    map[string]string{"X-API-Key": matrixFixtureCredential("55555555555555555555555555555555")},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:       "disabled expired credential continues none",
			decision:   RuntimeProxyKeyDecision{},
			headers:    map[string]string{"X-Goog-Api-Key": matrixFixtureCredential("66666666666666666666666666666666")},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyNone,
		},
		{
			name:       "disabled optional verifier unavailable continues unknown",
			verifyErr:  runtimeSnapshotUnavailableError(),
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyUnknown,
			wantVerified: []string{validCredential},
		},
		{
			name:       "disabled generic verifier failure continues unknown",
			verifyErr:  context.DeadlineExceeded,
			headers:    map[string]string{"Authorization": "Bearer " + validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyUnknown,
			wantVerified: []string{validCredential},
		},
		{
			name:       "disabled x-api-key valid identified",
			decision:   validDecision,
			headers:    map[string]string{"X-API-Key": validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyIdentified,
			wantEnforce: false, wantID: 42,
			wantVerified: []string{validCredential},
		},
		{
			name:       "disabled x-goog-api-key valid identified",
			decision:   validDecision,
			headers:    map[string]string{"X-Goog-Api-Key": validCredential},
			wantStatus: http.StatusOK, wantState: requestcontext.RuntimeProxyKeyIdentified,
			wantEnforce: false, wantID: 42,
			wantVerified: []string{validCredential},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := &fakeRuntimeAuthCache{authEnabled: tc.authEnabled, authErr: tc.authErr, decision: tc.decision, verifyErr: tc.verifyErr}
			service := newMatrixService(cache)
			status, body, capture := runMatrixRequest(t, service, tc.headers)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", status, tc.wantStatus, body)
			}
			hasAttribution := capture.hasAttribution
			if tc.wantStatus == http.StatusOK && !hasAttribution {
				t.Fatalf("permitted request must carry an attribution context")
			}
			if tc.wantStatus != http.StatusOK && hasAttribution {
				t.Fatalf("rejected request (status %d) must not reach attribution", tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK && hasAttribution {
				if capture.attribution.State != tc.wantState {
					t.Fatalf("attribution state = %q, want %q", capture.attribution.State, tc.wantState)
				}
				if capture.attribution.AuthEnforced != tc.wantEnforce {
					t.Fatalf("auth enforced = %v, want %v", capture.attribution.AuthEnforced, tc.wantEnforce)
				}
				if tc.wantState == requestcontext.RuntimeProxyKeyIdentified {
					if capture.attribution.Snapshot == nil {
						t.Fatal("identified attribution must carry a snapshot")
					}
					if capture.attribution.Snapshot.ID != tc.wantID {
						t.Fatalf("snapshot ID = %d, want %d", capture.attribution.Snapshot.ID, tc.wantID)
					}
					if tc.wantName != "" && capture.attribution.Snapshot.Name != tc.wantName {
						t.Fatalf("snapshot name = %q, want %q", capture.attribution.Snapshot.Name, tc.wantName)
					}
				} else if capture.attribution.Snapshot != nil {
					t.Fatalf("non-identified state %q must not carry a snapshot", capture.attribution.State)
				}
			}
			if tc.wantVerified != nil {
				verified := cache.verifiedKeys()
				if len(verified) != len(tc.wantVerified) {
					t.Fatalf("verified keys = %v, want %v", verified, tc.wantVerified)
				}
				for i := range verified {
					if verified[i] != tc.wantVerified[i] {
						t.Fatalf("verified keys = %v, want %v", verified, tc.wantVerified)
					}
				}
			}
			// Raw credentials never appear in error responses.
			for _, value := range tc.headers {
				credential := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "Bearer "), matrixFixtureCredential("")))
				if credential != "" && strings.Contains(body, credential) {
					t.Fatalf("error response leaked credential %q (body %q)", credential, body)
				}
			}
		})
	}
}
