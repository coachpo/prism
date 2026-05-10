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
	watchdogReasonManualOperatorPatch = "manual_operator_patch"
)

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
	if isSensitiveSnapshotKey(key) || isSensitiveHeaderName(key) {
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
