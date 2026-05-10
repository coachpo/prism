package sidecars

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const providerInventoryFailureItemKey = "__provider_inventory_error"

var sidecarProviderSyncEndpoints = []struct {
	Path        string
	ResponseKey string
}{
	{Path: "/gemini-api-key", ResponseKey: "gemini-api-key"},
	{Path: "/claude-api-key", ResponseKey: "claude-api-key"},
	{Path: "/codex-api-key", ResponseKey: "codex-api-key"},
	{Path: "/vertex-api-key", ResponseKey: "vertex-api-key"},
	{Path: "/openai-compatibility", ResponseKey: "openai-compatibility"},
}

func normalizeSidecarProviderSnapshots(sidecarID int, observedAt time.Time, providerKey string, payload map[string]json.RawMessage) ([]SidecarProviderSnapshotInput, error) {
	rawItems, ok := payload[providerKey]
	if !ok {
		return nil, invalidInputError("provider inventory response missing " + providerKey)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawItems, &items); err != nil {
		return nil, invalidInputError("provider inventory response must be an array")
	}
	inputs := make([]SidecarProviderSnapshotInput, 0, len(items))
	for index, raw := range items {
		fields, err := decodeSidecarSyncObject(raw)
		if err != nil {
			return nil, err
		}
		itemKey := providerSnapshotItemKey(fields, index)
		snapshot, err := normalizedProviderSnapshotJSON(providerKey, itemKey, index, fields)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, SidecarProviderSnapshotInput{
			SidecarID:       sidecarID,
			ProviderKey:     providerKey,
			ProviderItemKey: itemKey,
			Name:            stringPtrFromKeys(fields, "name"),
			Label:           stringPtrFromKeys(fields, "label"),
			Status:          stringPtrFromKeys(fields, "status"),
			Disabled:        boolPtrFromKey(fields, "disabled"),
			SnapshotJSON:    snapshot,
			ObservedAt:      observedAt,
		})
	}
	return inputs, nil
}

func normalizedProviderSnapshotJSON(providerKey string, itemKey string, index int, fields map[string]any) (json.RawMessage, error) {
	snapshot := map[string]any{
		"provider_key":      providerKey,
		"provider_item_key": itemKey,
		"item_index":        index,
		"proxy_url_present": providerProxyURLPresent(fields),
	}
	putStringMetadata(snapshot, fields, "name", "name")
	putStringMetadata(snapshot, fields, "label", "label")
	putStringMetadata(snapshot, fields, "status", "status")
	putStringMetadata(snapshot, fields, "prefix", "prefix")
	putStringMetadata(snapshot, fields, "base_url", "base_url", "base-url")
	putStringMetadata(snapshot, fields, "auth_index", "auth_index", "auth-index")
	putBoolMetadata(snapshot, fields, "disabled", "disabled")
	putBoolMetadata(snapshot, fields, "websockets", "websockets")
	putIntMetadata(snapshot, fields, "priority", "priority")

	if headerKeys, ok := providerHeaderKeysFromFields(fields); ok {
		snapshot["header_keys"] = headerKeys
	}
	if models, ok := providerModelsFromFields(fields); ok {
		snapshot["models"] = models
	}
	if excludedModels, ok := providerStringListFromFields(fields, "excluded-models", "excluded_models"); ok {
		snapshot["excluded_models"] = excludedModels
	}
	entries, entriesPresent, entriesSecretPresent, err := providerAPIKeyEntriesFromFields(fields)
	if err != nil {
		return nil, err
	}
	if entriesPresent {
		snapshot["api_key_entries"] = entries
	}
	secretPresent := providerSecretPresent(fields, "api-key", "api_key", "apiKey") || entriesSecretPresent
	snapshot["secret_present"] = secretPresent
	if secretPresent {
		snapshot["secret_masked"] = credentialMask
	}
	return marshalSidecarSnapshotJSON(snapshot)
}

func providerInventoryFailureSnapshotInput(sidecarID int, observedAt time.Time, providerKey string, path string, err error) SidecarProviderSnapshotInput {
	detail, code := redactedSidecarSyncError(err)
	name := providerKey
	status := sidecarConditionUnobservable
	snapshot := map[string]any{
		"provider_key":  providerKey,
		"provider_path": path,
		"condition":     sidecarConditionUnobservable,
		"error_code":    code,
		"error_detail":  detail,
		"partial":       true,
	}
	raw, marshalErr := marshalSidecarSnapshotJSON(snapshot)
	if marshalErr != nil {
		raw = json.RawMessage(`{"condition":"condition_unobservable"}`)
	}
	return SidecarProviderSnapshotInput{
		SidecarID:       sidecarID,
		ProviderKey:     providerKey,
		ProviderItemKey: providerInventoryFailureItemKey,
		Name:            &name,
		Status:          &status,
		SnapshotJSON:    raw,
		ObservedAt:      observedAt,
	}
}

func providerSnapshotItemKey(fields map[string]any, index int) string {
	for _, key := range []string{"auth-index", "auth_index", "name", "prefix", "base-url", "base_url"} {
		if value := trimmedStringValue(fields[key]); value != "" {
			return value
		}
	}
	return fmt.Sprintf("item-%d", index)
}

func putStringMetadata(snapshot map[string]any, fields map[string]any, outputKey string, inputKeys ...string) {
	if value := firstNonEmptyString(fields, inputKeys...); value != "" {
		snapshot[outputKey] = value
	}
}

func putBoolMetadata(snapshot map[string]any, fields map[string]any, outputKey string, inputKey string) {
	if value := boolPtrFromKey(fields, inputKey); value != nil {
		snapshot[outputKey] = *value
	}
}

func putIntMetadata(snapshot map[string]any, fields map[string]any, outputKey string, inputKey string) {
	if value := intPtrFromKey(fields, inputKey); value != nil {
		snapshot[outputKey] = *value
	}
}

func providerProxyURLPresent(fields map[string]any) bool {
	return firstNonEmptyString(fields, "proxy-url", "proxy_url") != ""
}

func providerHeaderKeysFromFields(fields map[string]any) ([]string, bool) {
	value, ok := valueFromKeys(fields, "headers")
	if !ok || value == nil {
		return nil, false
	}
	headers, ok := value.(map[string]any)
	if !ok {
		return []string{}, true
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys, true
}

func providerModelsFromFields(fields map[string]any) ([]map[string]string, bool) {
	value, ok := valueFromKeys(fields, "models")
	if !ok || value == nil {
		return nil, false
	}
	items, ok := value.([]any)
	if !ok {
		return []map[string]string{}, true
	}
	models := make([]map[string]string, 0, len(items))
	for _, item := range items {
		model := normalizedProviderModel(item)
		if len(model) > 0 {
			models = append(models, model)
		}
	}
	return models, true
}

func normalizedProviderModel(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]any:
		model := map[string]string{}
		if name := trimmedStringValue(typed["name"]); name != "" {
			model["name"] = name
		}
		if alias := trimmedStringValue(typed["alias"]); alias != "" {
			model["alias"] = alias
		}
		return model
	case string:
		if name := strings.TrimSpace(typed); name != "" {
			return map[string]string{"name": name}
		}
	}
	return nil
}

func providerStringListFromFields(fields map[string]any, keys ...string) ([]string, bool) {
	value, ok := valueFromKeys(fields, keys...)
	if !ok || value == nil {
		return nil, false
	}
	items, ok := value.([]any)
	if !ok {
		return []string{}, true
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if text := trimmedStringValue(item); text != "" {
			values = append(values, text)
		}
	}
	return values, true
}

func providerAPIKeyEntriesFromFields(fields map[string]any) ([]map[string]any, bool, bool, error) {
	value, ok := valueFromKeys(fields, "api-key-entries", "api_key_entries", "apiKeyEntries")
	if !ok || value == nil {
		return nil, false, false, nil
	}
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}, true, false, invalidInputError("provider api key entries must be an array")
	}
	entries := make([]map[string]any, 0, len(items))
	secretPresent := false
	for index, item := range items {
		entryFields, ok := item.(map[string]any)
		if !ok {
			return nil, true, false, invalidInputError("provider api key entry must be an object")
		}
		entry := normalizedProviderAPIKeyEntry(index, entryFields)
		if entrySecret, _ := entry["secret_present"].(bool); entrySecret {
			secretPresent = true
		}
		entries = append(entries, entry)
	}
	return entries, true, secretPresent, nil
}

func normalizedProviderAPIKeyEntry(index int, fields map[string]any) map[string]any {
	entry := map[string]any{
		"item_index":        index,
		"proxy_url_present": providerProxyURLPresent(fields),
	}
	putStringMetadata(entry, fields, "auth_index", "auth_index", "auth-index")
	putStringMetadata(entry, fields, "base_url", "base_url", "base-url")
	if headerKeys, ok := providerHeaderKeysFromFields(fields); ok {
		entry["header_keys"] = headerKeys
	}
	secretPresent := providerSecretPresent(fields, "api-key", "api_key", "apiKey")
	entry["secret_present"] = secretPresent
	if secretPresent {
		entry["secret_masked"] = credentialMask
	}
	return entry
}

func providerSecretPresent(fields map[string]any, keys ...string) bool {
	value, ok := valueFromKeys(fields, keys...)
	if !ok || value == nil {
		return false
	}
	if text := trimmedStringValue(value); text != "" {
		return true
	}
	_, isString := value.(string)
	return !isString
}

func valueFromKeys(fields map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := fields[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}
