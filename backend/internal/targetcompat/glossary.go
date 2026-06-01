package targetcompat

import "strings"

const (
	AccessTargetTypeModel          = "model"
	PersistedTerminalTargetType    = "connection"
	ConnectionIDFieldName          = "connection_id"
	TerminalTargetIDFieldName      = "terminal_target_id"
	ConnectionObjectFieldName      = "connection"
	TerminalTargetObjectFieldName  = "terminal_target"
	OwnerScopedConnectionRoutePath = "/api/models/{model_config_id}/connections"
)

func NormalizeAccessTargetType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsModelAccessTargetType(value string) bool {
	return NormalizeAccessTargetType(value) == AccessTargetTypeModel
}

func IsTerminalTargetAccessTargetType(value string) bool {
	return NormalizeAccessTargetType(value) == PersistedTerminalTargetType
}

func IsSupportedAccessTargetType(value string) bool {
	return IsModelAccessTargetType(value) || IsTerminalTargetAccessTargetType(value)
}
