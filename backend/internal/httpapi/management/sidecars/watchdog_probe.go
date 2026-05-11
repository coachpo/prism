package sidecars

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	watchdogProbeStatusSucceeded                  = "probe_succeeded"
	watchdogProbeStatusFailedTimeout              = "probe_failed_timeout"
	watchdogProbeStatusFailedManagementAuth       = "probe_failed_management_auth"
	watchdogProbeStatusFailedToken                = "probe_failed_token"
	watchdogProbeStatusFailedStatus               = "probe_failed_status"
	watchdogProbeStatusFailedParse                = "probe_failed_parse"
	watchdogProbeStatusFailedTransport            = "probe_failed_transport"
	watchdogProbeStatusSkippedUnsupportedProvider = "probe_skipped_unsupported_provider"

	watchdogChatGPTUsageURL       = "https://chatgpt.com/backend-api/wham/usage"
	watchdogChatGPTUsageUserAgent = "codex_cli_rs/0.76.0"
)

type sidecarWatchdogProbeSpec struct {
	ProviderKey string
	Request     CLIProxyAPICallRequest
}

type sidecarWatchdogProbeClassification struct {
	Status             string
	UpstreamStatusCode *int
	QuotaExceeded      bool
	QuotaReason        *string
	QuotaResetAt       *time.Time
	BlockingWindow     *string
	Windows            []sidecarWatchdogQuotaWindow
	ErrorCode          *string
}

type sidecarWatchdogQuotaNormalization struct {
	QuotaExceeded  bool
	QuotaReason    *string
	QuotaResetAt   *time.Time
	BlockingWindow *string
	Windows        []sidecarWatchdogQuotaWindow
}

type sidecarWatchdogQuotaWindow struct {
	Source             string     `json:"source"`
	WindowType         string     `json:"window_type"`
	UsedPercent        *float64   `json:"used_percent,omitempty"`
	LimitReached       *bool      `json:"limit_reached,omitempty"`
	Allowed            *bool      `json:"allowed,omitempty"`
	ResetAt            *time.Time `json:"reset_at,omitempty"`
	LimitWindowSeconds *int       `json:"limit_window_seconds,omitempty"`
	Blocking           bool       `json:"blocking,omitempty"`
}

type sidecarWatchdogUsagePayload struct {
	RateLimit            sidecarWatchdogUsageRateLimit             `json:"rate_limit"`
	AdditionalRateLimits []sidecarWatchdogAdditionalUsageRateLimit `json:"additional_rate_limits"`
}

type sidecarWatchdogAdditionalUsageRateLimit struct {
	RateLimit sidecarWatchdogUsageRateLimit `json:"rate_limit"`
}

type sidecarWatchdogUsageRateLimit struct {
	Allowed         *bool                       `json:"allowed"`
	LimitReached    *bool                       `json:"limit_reached"`
	PrimaryWindow   *sidecarWatchdogUsageWindow `json:"primary_window"`
	SecondaryWindow *sidecarWatchdogUsageWindow `json:"secondary_window"`
}

type sidecarWatchdogUsageWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitReached       *bool    `json:"limit_reached"`
	Allowed            *bool    `json:"allowed"`
	Exhausted          *bool    `json:"exhausted"`
	Blocked            *bool    `json:"blocked"`
	ResetAt            *int64   `json:"reset_at"`
	ResetAfterSeconds  *int64   `json:"reset_after_seconds"`
	LimitWindowSeconds *int     `json:"limit_window_seconds"`
}

func buildSidecarWatchdogProbeSpec(providerKey string, authIndex string) (sidecarWatchdogProbeSpec, bool) {
	normalizedProvider := normalizedSidecarWatchdogProbeProviderKey(providerKey)
	if !sidecarWatchdogProbeProviderSupported(normalizedProvider) {
		return sidecarWatchdogProbeSpec{}, false
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return sidecarWatchdogProbeSpec{}, false
	}
	return sidecarWatchdogProbeSpec{
		ProviderKey: normalizedProvider,
		Request: CLIProxyAPICallRequest{
			AuthIndex: authIndex,
			Method:    http.MethodGet,
			URL:       watchdogChatGPTUsageURL,
			Header: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
				"User-Agent":    watchdogChatGPTUsageUserAgent,
			},
		},
	}, true
}

func normalizedSidecarWatchdogProbeProviderKey(providerKey string) string {
	return strings.ToLower(strings.TrimSpace(providerKey))
}

func sidecarWatchdogProbeProviderSupported(providerKey string) bool {
	switch normalizedSidecarWatchdogProbeProviderKey(providerKey) {
	case "codex", "chatgpt":
		return true
	default:
		return false
	}
}

func classifySidecarWatchdogProbe(providerKey string, response CLIProxyAPICallResponse, err error, now time.Time, fallbackCooldownSeconds int) sidecarWatchdogProbeClassification {
	if !sidecarWatchdogProbeProviderSupported(providerKey) {
		return sidecarWatchdogProbeClassification{Status: watchdogProbeStatusSkippedUnsupportedProvider}
	}
	if err != nil {
		return classifySidecarWatchdogProbeError(err)
	}
	if response.StatusCode == 0 && sidecarWatchdogProbeBodyReportsTokenFailure(response.Body) {
		return sidecarWatchdogProbeClassification{Status: watchdogProbeStatusFailedToken, ErrorCode: stringPtrFromNonEmpty("token_substitution_failed")}
	}
	if response.StatusCode != http.StatusOK {
		statusCode := response.StatusCode
		return sidecarWatchdogProbeClassification{Status: watchdogProbeStatusFailedStatus, UpstreamStatusCode: &statusCode}
	}
	normalized, parseErr := parseSidecarWatchdogUsageBody(response.Body, now, fallbackCooldownSeconds)
	if parseErr != nil {
		return sidecarWatchdogProbeClassification{Status: watchdogProbeStatusFailedParse, ErrorCode: stringPtrFromNonEmpty("usage_body_parse_failed")}
	}
	return sidecarWatchdogProbeClassification{
		Status:         watchdogProbeStatusSucceeded,
		QuotaExceeded:  normalized.QuotaExceeded,
		QuotaReason:    cloneStringPtr(normalized.QuotaReason),
		QuotaResetAt:   cloneTimePtr(normalized.QuotaResetAt),
		BlockingWindow: cloneStringPtr(normalized.BlockingWindow),
		Windows:        cloneSidecarWatchdogQuotaWindows(normalized.Windows),
	}
}

func classifySidecarWatchdogProbeError(err error) sidecarWatchdogProbeClassification {
	status := watchdogProbeStatusFailedTransport
	var clientErr *CLIProxyClientError
	if errors.Is(err, context.DeadlineExceeded) {
		status = watchdogProbeStatusFailedTimeout
	} else if errors.As(err, &clientErr) {
		switch clientErr.Code {
		case CLIProxyErrorTimeout:
			status = watchdogProbeStatusFailedTimeout
		case CLIProxyErrorInvalidManagementAuth:
			status = watchdogProbeStatusFailedManagementAuth
		}
	}
	return sidecarWatchdogProbeClassification{Status: status, ErrorCode: sidecarWatchdogProbeErrorCode(err)}
}

func parseSidecarWatchdogUsageBody(rawBody json.RawMessage, now time.Time, fallbackCooldownSeconds int) (sidecarWatchdogQuotaNormalization, error) {
	bodyBytes := bytes.TrimSpace(rawBody)
	if len(bodyBytes) == 0 {
		return sidecarWatchdogQuotaNormalization{}, errors.New("api-call body is empty")
	}
	var usageBody string
	if err := json.Unmarshal(bodyBytes, &usageBody); err == nil {
		bodyBytes = []byte(strings.TrimSpace(usageBody))
		if len(bodyBytes) == 0 {
			return sidecarWatchdogQuotaNormalization{}, errors.New("usage body is empty")
		}
	}
	var payload sidecarWatchdogUsagePayload
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return sidecarWatchdogQuotaNormalization{}, fmt.Errorf("usage body JSON is malformed: %w", err)
	}
	return normalizeSidecarWatchdogUsagePayload(payload, now, fallbackCooldownSeconds)
}

func normalizeSidecarWatchdogUsagePayload(payload sidecarWatchdogUsagePayload, now time.Time, fallbackCooldownSeconds int) (sidecarWatchdogQuotaNormalization, error) {
	fallbackCooldownSeconds = normalizedProbeFallbackCooldownSeconds(fallbackCooldownSeconds)
	windows := make([]sidecarWatchdogQuotaWindow, 0, 4+len(payload.AdditionalRateLimits)*2)
	appendSidecarWatchdogRateLimitWindows(&windows, "rate_limit", payload.RateLimit, now, fallbackCooldownSeconds)
	for index, additional := range payload.AdditionalRateLimits {
		source := fmt.Sprintf("additional_rate_limits[%d].rate_limit", index)
		appendSidecarWatchdogRateLimitWindows(&windows, source, additional.RateLimit, now, fallbackCooldownSeconds)
	}
	if len(windows) == 0 {
		return sidecarWatchdogQuotaNormalization{}, errors.New("usage body did not include quota windows")
	}
	result := sidecarWatchdogQuotaNormalization{Windows: windows}
	var latestReset time.Time
	for _, window := range windows {
		if !window.Blocking || window.ResetAt == nil {
			continue
		}
		if result.QuotaResetAt == nil || window.ResetAt.After(latestReset) {
			latestReset = window.ResetAt.UTC()
			result.QuotaExceeded = true
			result.QuotaResetAt = &latestReset
			result.BlockingWindow = stringPtrFromNonEmpty(window.WindowType)
		}
	}
	if result.QuotaExceeded {
		result.QuotaReason = stringPtrFromNonEmpty(fmt.Sprintf("%s:%s", watchdogReasonQuotaExceeded, *result.BlockingWindow))
	}
	return result, nil
}

func appendSidecarWatchdogRateLimitWindows(windows *[]sidecarWatchdogQuotaWindow, source string, rateLimit sidecarWatchdogUsageRateLimit, now time.Time, fallbackCooldownSeconds int) {
	appendSidecarWatchdogUsageWindow(windows, source+".primary_window", rateLimit, rateLimit.PrimaryWindow, now, fallbackCooldownSeconds)
	appendSidecarWatchdogUsageWindow(windows, source+".secondary_window", rateLimit, rateLimit.SecondaryWindow, now, fallbackCooldownSeconds)
}

func appendSidecarWatchdogUsageWindow(windows *[]sidecarWatchdogQuotaWindow, source string, rateLimit sidecarWatchdogUsageRateLimit, window *sidecarWatchdogUsageWindow, now time.Time, fallbackCooldownSeconds int) {
	if window == nil {
		return
	}
	limitReached := boolPtrValue(rateLimit.LimitReached) || boolPtrValue(window.LimitReached) || boolPtrValue(window.Exhausted) || boolPtrValue(window.Blocked)
	allowedFalse := boolPtrFalse(rateLimit.Allowed) || boolPtrFalse(window.Allowed)
	blocking := limitReached || allowedFalse
	normalized := sidecarWatchdogQuotaWindow{
		Source:             source,
		WindowType:         sidecarWatchdogQuotaWindowType(window.LimitWindowSeconds),
		UsedPercent:        cloneFloat64Ptr(window.UsedPercent),
		LimitWindowSeconds: cloneIntPtr(window.LimitWindowSeconds),
		Blocking:           blocking,
	}
	if limitReached {
		normalized.LimitReached = boolPtrFromValue(true)
	} else if window.LimitReached != nil {
		normalized.LimitReached = cloneBoolPtr(window.LimitReached)
	} else if rateLimit.LimitReached != nil {
		normalized.LimitReached = cloneBoolPtr(rateLimit.LimitReached)
	}
	if allowedFalse {
		normalized.Allowed = boolPtrFromValue(false)
	} else if window.Allowed != nil {
		normalized.Allowed = cloneBoolPtr(window.Allowed)
	} else if rateLimit.Allowed != nil {
		normalized.Allowed = cloneBoolPtr(rateLimit.Allowed)
	}
	normalized.ResetAt = sidecarWatchdogUsageWindowResetAt(window, now, fallbackCooldownSeconds, blocking)
	*windows = append(*windows, normalized)
}

func sidecarWatchdogQuotaWindowType(limitWindowSeconds *int) string {
	seconds := intPtrValue(limitWindowSeconds)
	switch seconds {
	case 18000:
		return "five_hour"
	case 604800:
		return "weekly"
	default:
		return fmt.Sprintf("custom_%d", seconds)
	}
}

func sidecarWatchdogUsageWindowResetAt(window *sidecarWatchdogUsageWindow, now time.Time, fallbackCooldownSeconds int, blocking bool) *time.Time {
	if window.ResetAt != nil {
		resetAt := time.Unix(*window.ResetAt, 0).UTC()
		if resetAt.After(now.UTC()) {
			return &resetAt
		}
	}
	if window.ResetAfterSeconds != nil && *window.ResetAfterSeconds > 0 {
		resetAt := now.UTC().Add(time.Duration(*window.ResetAfterSeconds) * time.Second)
		return &resetAt
	}
	if blocking {
		resetAt := now.UTC().Add(time.Duration(fallbackCooldownSeconds) * time.Second)
		return &resetAt
	}
	return nil
}

func sidecarWatchdogProbeBodyReportsTokenFailure(rawBody json.RawMessage) bool {
	bodyBytes := bytes.TrimSpace(rawBody)
	if len(bodyBytes) == 0 {
		return false
	}
	var bodyString string
	if err := json.Unmarshal(bodyBytes, &bodyString); err == nil {
		bodyBytes = []byte(strings.TrimSpace(bodyString))
	}
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return false
	}
	var tokenPayload struct {
		Code      string `json:"code"`
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(bodyBytes, &tokenPayload); err != nil {
		return false
	}
	for _, value := range []string{tokenPayload.Code, tokenPayload.ErrorCode, tokenPayload.Error, tokenPayload.Type} {
		if sidecarWatchdogTokenFailureCode(value) {
			return true
		}
	}
	return false
}

func sidecarWatchdogTokenFailureCode(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "token_substitution_failed", "token_substitution_error", "token_replacement_failed":
		return true
	default:
		return false
	}
}

func sidecarWatchdogProbeErrorCode(err error) *string {
	if err == nil {
		return nil
	}
	var clientErr *CLIProxyClientError
	if errors.As(err, &clientErr) && clientErr.Code != "" {
		return stringPtrFromNonEmpty(string(clientErr.Code))
	}
	return stringPtrFromNonEmpty(watchdogProbeStatusFailedTransport)
}

func normalizedProbeFallbackCooldownSeconds(seconds int) int {
	if seconds <= 0 {
		return DefaultFallbackCooldownSeconds
	}
	return seconds
}

func boolPtrValue(value *bool) bool {
	return value != nil && *value
}

func boolPtrFalse(value *bool) bool {
	return value != nil && !*value
}

func boolPtrFromValue(value bool) *bool {
	return &value
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneSidecarWatchdogQuotaWindows(windows []sidecarWatchdogQuotaWindow) []sidecarWatchdogQuotaWindow {
	if windows == nil {
		return nil
	}
	copy := make([]sidecarWatchdogQuotaWindow, len(windows))
	for index, window := range windows {
		copy[index] = sidecarWatchdogQuotaWindow{
			Source:             window.Source,
			WindowType:         window.WindowType,
			UsedPercent:        cloneFloat64Ptr(window.UsedPercent),
			LimitReached:       cloneBoolPtr(window.LimitReached),
			Allowed:            cloneBoolPtr(window.Allowed),
			ResetAt:            cloneTimePtr(window.ResetAt),
			LimitWindowSeconds: cloneIntPtr(window.LimitWindowSeconds),
			Blocking:           window.Blocking,
		}
	}
	return copy
}
