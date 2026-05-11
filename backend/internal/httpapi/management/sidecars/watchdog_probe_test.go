package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWatchdogProbeStatusSpecRequestHeaders(t *testing.T) {
	for _, provider := range []string{"codex", " ChatGPT "} {
		spec, ok := buildSidecarWatchdogProbeSpec(provider, " auth_001 ")
		if !ok {
			t.Fatalf("expected provider %q to produce a probe spec", provider)
		}
		if spec.ProviderKey != normalizedSidecarWatchdogProbeProviderKey(provider) {
			t.Fatalf("expected normalized provider key, got %q", spec.ProviderKey)
		}
		if spec.Request.AuthIndex != "auth_001" || spec.Request.Method != http.MethodGet || spec.Request.URL != watchdogChatGPTUsageURL {
			t.Fatalf("unexpected probe request: %+v", spec.Request)
		}
		if spec.Request.Header["Authorization"] != "Bearer $TOKEN$" || spec.Request.Header["Content-Type"] != "application/json" || spec.Request.Header["User-Agent"] != watchdogChatGPTUsageUserAgent {
			t.Fatalf("unexpected probe headers: %+v", spec.Request.Header)
		}
		if len(spec.Request.Header) != 3 {
			t.Fatalf("probe headers must contain only required safe keys, got %+v", spec.Request.Header)
		}
		for key := range spec.Request.Header {
			if strings.EqualFold(key, "Chatgpt-Account-Id") {
				t.Fatalf("probe spec must not send account id header: %+v", spec.Request.Header)
			}
		}
	}
	for _, provider := range []string{"gemini", "codex-api-key", "openai"} {
		if _, ok := buildSidecarWatchdogProbeSpec(provider, "auth_001"); ok {
			t.Fatalf("unsupported provider %q produced a probe spec", provider)
		}
	}
}

func TestWatchdogProbeUnsupportedProviderClassification(t *testing.T) {
	result := classifySidecarWatchdogProbe("gemini", CLIProxyAPICallResponse{}, nil, time.Now(), 60)
	if result.Status != watchdogProbeStatusSkippedUnsupportedProvider {
		t.Fatalf("expected unsupported provider skip, got %+v", result)
	}
}

func TestWatchdogQuotaParserNormalizesWindows(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"rate_limit": map[string]any{
			"allowed": false,
			"primary_window": map[string]any{
				"used_percent": 90.5, "limit_window_seconds": 18000, "reset_at": now.Add(5 * time.Hour).Unix(),
			},
			"secondary_window": map[string]any{
				"limit_reached": true, "limit_window_seconds": 604800, "reset_after_seconds": 604800,
			},
		},
		"additional_rate_limits": []any{
			map[string]any{"rate_limit": map[string]any{"allowed": true, "primary_window": map[string]any{"used_percent": 100, "limit_window_seconds": 42, "reset_after_seconds": 60}}},
			map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"allowed": false, "limit_window_seconds": 123, "reset_at": now.Add(2 * time.Hour).Unix()}}},
		},
	}

	normalized, err := parseSidecarWatchdogUsageBody(wrappedProbeUsagePayloadForTest(t, payload), now, 3600)
	if err != nil {
		t.Fatalf("parse usage body: %v", err)
	}
	if len(normalized.Windows) != 4 {
		t.Fatalf("expected four normalized windows, got %+v", normalized.Windows)
	}
	wantTypes := []string{"five_hour", "weekly", "custom_42", "custom_123"}
	for index, want := range wantTypes {
		if normalized.Windows[index].WindowType != want {
			t.Fatalf("window %d type mismatch: got %q want %q", index, normalized.Windows[index].WindowType, want)
		}
	}
	if normalized.Windows[2].Blocking {
		t.Fatalf("high used_percent without exhaustion flags must not block: %+v", normalized.Windows[2])
	}
	wantReset := now.Add(604800 * time.Second)
	if !normalized.QuotaExceeded || normalized.QuotaResetAt == nil || !normalized.QuotaResetAt.Equal(wantReset) {
		t.Fatalf("expected latest blocking reset %s, got %+v", wantReset, normalized)
	}
	if normalized.QuotaReason == nil || *normalized.QuotaReason != "quota_exceeded:weekly" || normalized.BlockingWindow == nil || *normalized.BlockingWindow != "weekly" {
		t.Fatalf("expected weekly quota reason, got %+v", normalized)
	}
}

func TestWatchdogQuotaWindowHighUsageDoesNotBlock(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{"rate_limit": map[string]any{"allowed": true, "primary_window": map[string]any{"used_percent": 100, "limit_window_seconds": 18000, "reset_after_seconds": 60}}}
	normalized, err := parseSidecarWatchdogUsageBody(wrappedProbeUsagePayloadForTest(t, payload), now, 3600)
	if err != nil {
		t.Fatalf("parse high usage body: %v", err)
	}
	if normalized.QuotaExceeded || normalized.QuotaReason != nil || normalized.QuotaResetAt != nil {
		t.Fatalf("high-but-allowed usage must not become quota exhaustion: %+v", normalized)
	}
	if len(normalized.Windows) != 1 || normalized.Windows[0].Blocking {
		t.Fatalf("expected one non-blocking window, got %+v", normalized.Windows)
	}
}

func TestWatchdogQuotaParserAcceptsRawObjectBody(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"allowed": true, "limit_window_seconds": 18000}}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal raw body fixture: %v", err)
	}
	normalized, err := parseSidecarWatchdogUsageBody(body, now, 3600)
	if err != nil {
		t.Fatalf("parse raw object body: %v", err)
	}
	if len(normalized.Windows) != 1 || normalized.Windows[0].WindowType != "five_hour" {
		t.Fatalf("expected raw object body to normalize one five_hour window, got %+v", normalized.Windows)
	}
}

func TestWatchdogQuotaParserRejectsMalformedAndMissingWindows(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	malformedBody, err := json.Marshal("{")
	if err != nil {
		t.Fatalf("marshal malformed fixture: %v", err)
	}
	tests := []struct {
		name string
		body json.RawMessage
	}{
		{name: "malformed JSON string", body: malformedBody},
		{name: "missing windows", body: wrappedProbeUsagePayloadForTest(t, map[string]any{"rate_limit": map[string]any{}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseSidecarWatchdogUsageBody(tt.body, now, 3600); err == nil {
				t.Fatalf("expected parse error for %s", tt.name)
			}
		})
	}
}

func TestWatchdogQuotaWindowPastResetUsesFallback(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"limit_reached":        true,
				"limit_window_seconds": 18000,
				"reset_at":             now.Add(-time.Hour).Unix(),
			},
		},
	}
	normalized, err := parseSidecarWatchdogUsageBody(wrappedProbeUsagePayloadForTest(t, payload), now, 90)
	if err != nil {
		t.Fatalf("parse past reset body: %v", err)
	}
	wantReset := now.Add(90 * time.Second)
	if normalized.QuotaResetAt == nil || !normalized.QuotaResetAt.Equal(wantReset) {
		t.Fatalf("expected fallback reset %s, got %+v", wantReset, normalized)
	}
}

func TestWatchdogProbeClassificationFailureStatuses(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	successBody := wrappedProbeUsagePayloadForTest(t, map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"allowed": true, "limit_window_seconds": 18000}}})
	malformedUsage, err := json.Marshal("{")
	if err != nil {
		t.Fatalf("marshal malformed usage: %v", err)
	}
	tokenBody := wrappedProbeUsagePayloadForTest(t, map[string]any{"error_code": "token_substitution_failed"})
	upstreamErrorBody := wrappedProbeUsagePayloadForTest(t, map[string]any{"error": "invalid token"})
	tests := []struct {
		name           string
		response       CLIProxyAPICallResponse
		err            error
		wantStatus     string
		wantStatusCode *int
	}{
		{name: "success", response: CLIProxyAPICallResponse{StatusCode: http.StatusOK, Body: successBody}, wantStatus: watchdogProbeStatusSucceeded},
		{name: "context timeout", err: context.DeadlineExceeded, wantStatus: watchdogProbeStatusFailedTimeout},
		{name: "client timeout", err: &CLIProxyClientError{Code: CLIProxyErrorTimeout, Path: "/api-call"}, wantStatus: watchdogProbeStatusFailedTimeout},
		{name: "management auth", err: &CLIProxyClientError{Code: CLIProxyErrorInvalidManagementAuth, StatusCode: http.StatusForbidden, Path: "/api-call"}, wantStatus: watchdogProbeStatusFailedManagementAuth},
		{name: "transport", err: errors.New("dial tcp failed"), wantStatus: watchdogProbeStatusFailedTransport},
		{name: "token substitution", response: CLIProxyAPICallResponse{StatusCode: 0, Body: tokenBody}, wantStatus: watchdogProbeStatusFailedToken},
		{name: "wrapped 401", response: CLIProxyAPICallResponse{StatusCode: http.StatusUnauthorized, Body: upstreamErrorBody}, wantStatus: watchdogProbeStatusFailedStatus, wantStatusCode: intPtrForProbeTest(http.StatusUnauthorized)},
		{name: "wrapped 403", response: CLIProxyAPICallResponse{StatusCode: http.StatusForbidden, Body: upstreamErrorBody}, wantStatus: watchdogProbeStatusFailedStatus, wantStatusCode: intPtrForProbeTest(http.StatusForbidden)},
		{name: "wrapped 429", response: CLIProxyAPICallResponse{StatusCode: http.StatusTooManyRequests, Body: upstreamErrorBody}, wantStatus: watchdogProbeStatusFailedStatus, wantStatusCode: intPtrForProbeTest(http.StatusTooManyRequests)},
		{name: "wrapped 500", response: CLIProxyAPICallResponse{StatusCode: http.StatusInternalServerError, Body: upstreamErrorBody}, wantStatus: watchdogProbeStatusFailedStatus, wantStatusCode: intPtrForProbeTest(http.StatusInternalServerError)},
		{name: "parse failure", response: CLIProxyAPICallResponse{StatusCode: http.StatusOK, Body: malformedUsage}, wantStatus: watchdogProbeStatusFailedParse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySidecarWatchdogProbe("codex", tt.response, tt.err, now, 3600)
			if got.Status != tt.wantStatus {
				t.Fatalf("status mismatch: got %s want %s result=%+v", got.Status, tt.wantStatus, got)
			}
			if tt.wantStatusCode != nil {
				if got.UpstreamStatusCode == nil || *got.UpstreamStatusCode != *tt.wantStatusCode {
					t.Fatalf("upstream status mismatch: got %+v want %d", got.UpstreamStatusCode, *tt.wantStatusCode)
				}
			}
		})
	}
}

func wrappedProbeUsagePayloadForTest(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal usage payload: %v", err)
	}
	wrapped, err := json.Marshal(string(body))
	if err != nil {
		t.Fatalf("marshal wrapped usage payload: %v", err)
	}
	return wrapped
}

func intPtrForProbeTest(value int) *int {
	return &value
}
