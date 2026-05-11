package sidecars

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const (
	watchdogActionOperatorPatch       = "operator_patch"
	watchdogActionQuotaHoldExtended   = "quota_hold_extended"
	watchdogReasonManualOperatorPatch = "manual_operator_patch"
)

func publicWatchdogActionType(action SidecarWatchdogAction) string {
	actionType := strings.TrimSpace(action.ActionType)
	if isQuotaHoldExtensionAction(action) {
		return watchdogActionQuotaHoldExtended
	}
	return actionType
}

func publicWatchdogActionReason(actionType string, action SidecarWatchdogAction) *string {
	if isProbeActionHistoryType(actionType) {
		return stringPtrFromNonEmpty(actionType)
	}
	if actionType == watchdogActionQuotaHoldExtended {
		reason := strings.TrimSpace(stringValue(action.Reason))
		if actionHistoryReasonCodeSafe(reason) && strings.HasPrefix(reason, watchdogReasonQuotaExceeded) {
			return stringPtrFromNonEmpty(reason)
		}
		return stringPtrFromNonEmpty(watchdogActionQuotaHoldExtended)
	}
	return publicSafeActionHistoryText(action.Reason)
}

func publicWatchdogActionErrorMessage(actionType string, action SidecarWatchdogAction) *string {
	if actionType == watchdogActionQuotaHoldExtended {
		return nil
	}
	if isProbeActionHistoryType(actionType) {
		return publicProbeActionErrorMessage(actionType, action.ErrorMessage)
	}
	return publicSafeActionHistoryText(action.ErrorMessage)
}

func isQuotaHoldExtensionAction(action SidecarWatchdogAction) bool {
	reason := strings.TrimSpace(stringValue(action.Reason))
	return strings.TrimSpace(action.ActionType) == watchdogActionRestoreSkippedUnhealthy && action.HoldID != nil && strings.HasPrefix(reason, watchdogReasonQuotaExceeded)
}

func isProbeActionHistoryType(actionType string) bool {
	return actionType == watchdogProbeStatusSucceeded || actionType == watchdogProbeStatusSkippedUnsupportedProvider || strings.HasPrefix(actionType, "probe_failed_")
}

func publicProbeActionErrorMessage(actionType string, value *string) *string {
	if !strings.HasPrefix(actionType, "probe_failed_") {
		return nil
	}
	message := strings.TrimSpace(stringValue(value))
	if message == "" {
		return nil
	}
	if statusCode := actionHistoryStatusCode(message); statusCode != "" {
		return stringPtrFromNonEmpty(actionType + " status=" + statusCode)
	}
	if actionHistoryTextLooksSensitive(message) || !strings.HasPrefix(message, actionType) {
		return stringPtrFromNonEmpty(actionType)
	}
	return publicSafeActionHistoryText(&message)
}

func publicSafeActionHistoryText(value *string) *string {
	text := strings.TrimSpace(stringValue(value))
	if text == "" {
		return nil
	}
	if len(text) > 500 {
		text = text[:500]
	}
	if actionHistoryTextLooksSensitive(text) {
		return stringPtrFromNonEmpty("redacted-by-prism")
	}
	return stringPtrFromNonEmpty(text)
}

func actionHistoryTextLooksSensitive(value string) bool {
	normalized := strings.ToLower(value)
	markers := []string{"authorization", "bearer ", "cookie", "set-cookie", "x-api-key", "apikey", "api_key", "access_token", "refresh_token", "chatgpt-account-id", "account_id", "account id", "\"body\"", "body:", "\"headers\"", "headers:", "raw-", "secret"}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return strings.Contains(normalized, "@")
}

func actionHistoryReasonCodeSafe(value string) bool {
	if value == "" || actionHistoryTextLooksSensitive(value) {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == ':' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func actionHistoryStatusCode(value string) string {
	marker := "status="
	index := strings.Index(strings.ToLower(value), marker)
	if index < 0 {
		return ""
	}
	tail := value[index+len(marker):]
	digits := strings.Builder{}
	for _, char := range tail {
		if char < '0' || char > '9' {
			break
		}
		digits.WriteRune(char)
	}
	code := digits.String()
	if len(code) != 3 {
		return ""
	}
	return code
}

type operatorPatchActionContext struct {
	SidecarID      int
	AuthSnapshotID *int
	AuthID         string
	AuthIndex      *string
	Provider       *string
	HoldID         *int
	HoldUntil      *time.Time
	Route          string
	Fields         []string
	Response       any
}

func operatorPatchContextFromSnapshot(snapshot SidecarAuthSnapshot, route string, fields []string) operatorPatchActionContext {
	ctx := operatorPatchActionContext{SidecarID: snapshot.SidecarID, AuthID: snapshot.AuthID, AuthIndex: cloneStringPtr(snapshot.AuthIndex), Provider: cloneStringPtr(snapshot.Provider), Route: route, Fields: append([]string(nil), fields...)}
	if snapshot.ID > 0 {
		ctx.AuthSnapshotID = &snapshot.ID
	}
	return ctx
}

func (ctx operatorPatchActionContext) withHold(hold *SidecarWatchdogHold) operatorPatchActionContext {
	if hold == nil || hold.ID <= 0 {
		return ctx
	}
	ctx.HoldID = &hold.ID
	ctx.HoldUntil = cloneTimePtr(hold.ManualPauseUntil)
	return ctx
}

func (s *Service) recordOperatorPatchAction(ctx context.Context, actionCtx operatorPatchActionContext, status string, patchErr error, now time.Time) (SidecarWatchdogAction, error) {
	reason := operatorPatchActionReason(actionCtx, patchErr)
	input := SidecarWatchdogActionInput{SidecarID: actionCtx.SidecarID, AuthSnapshotID: cloneIntPtr(actionCtx.AuthSnapshotID), HoldID: cloneIntPtr(actionCtx.HoldID), AuthID: stringPtrFromNonEmpty(actionCtx.AuthID), AuthIndex: cloneStringPtr(actionCtx.AuthIndex), Provider: cloneStringPtr(actionCtx.Provider), ActionType: watchdogActionOperatorPatch, Reason: &reason, HoldUntil: cloneTimePtr(actionCtx.HoldUntil), Status: status}
	if patchErr != nil {
		message := watchdogErrorMessage(patchErr)
		input.ErrorMessage = &message
	}
	return s.createWatchdogAction(ctx, input, now)
}

func operatorPatchActionReason(actionCtx operatorPatchActionContext, patchErr error) string {
	fields := append([]string(nil), actionCtx.Fields...)
	sort.Strings(fields)
	summary := map[string]any{
		"request": map[string]any{
			"route":  strings.TrimSpace(actionCtx.Route),
			"fields": fields,
		},
	}
	if actionCtx.Response != nil {
		summary["response"] = redactedActionValue("", actionCtx.Response)
	}
	if patchErr != nil {
		summary["error"] = watchdogErrorMessage(patchErr)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return `{"request":{"route":"operator_patch"}}`
	}
	if len(payload) > 2000 {
		payload = payload[:2000]
	}
	return string(payload)
}

func redactedActionValue(key string, value any) any {
	if isSensitiveSnapshotKey(key) || isSensitiveHeaderName(key) || isSensitiveActionPayloadKey(key) {
		return "redacted-by-prism"
	}
	switch typed := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			copy[nestedKey] = redactedActionValue(nestedKey, nestedValue)
		}
		return copy
	case map[string]string:
		copy := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			copy[nestedKey] = redactedActionValue(nestedKey, nestedValue)
		}
		return copy
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, redactedActionValue("", item))
		}
		return items
	default:
		return value
	}
}

func isSensitiveActionPayloadKey(key string) bool {
	switch normalizedSnapshotKey(key) {
	case "body", "header", "headers", "accountid", "email":
		return true
	default:
		return false
	}
}

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isSensitiveHeaderName(name string) bool {
	normalized := normalizedSnapshotKey(name)
	return strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "auth") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "xapikey") ||
		strings.Contains(normalized, "oauth") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential")
}
