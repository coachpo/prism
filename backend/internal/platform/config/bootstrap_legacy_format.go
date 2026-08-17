package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func (r *bootstrapRuntimeRouting) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for field := range fields {
		// Legacy startup field kept only so existing config.json files continue to parse after removal.
		if field == "openaiTerminalTranslationMode" {
			continue
		}
		return fmt.Errorf("json: unknown field %q", field)
	}
	return nil
}

func decodeBootstrapConfig(raw []byte) (bootstrapConfigDocument, error) {
	if err := rejectRemovedRuntimeTransportField(raw); err != nil {
		return bootstrapConfigDocument{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document bootstrapConfigDocument
	if err := decoder.Decode(&document); err != nil {
		return bootstrapConfigDocument{}, fmt.Errorf("decode bootstrap config JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return bootstrapConfigDocument{}, fmt.Errorf("bootstrap config JSON must contain a single object")
		}
		return bootstrapConfigDocument{}, fmt.Errorf("decode bootstrap config JSON: %w", err)
	}
	return document, nil
}

// rejectRemovedRuntimeTransportField translates a legacy runtime.transport
// block into a readable migration error instead of the bare unknown-field
// error DisallowUnknownFields would otherwise produce. The runtime.transport
// config section was removed outright (no compatibility shell), so existing
// config.json files must delete the section as part of the same upgrade
// window that deploys this build.
func rejectRemovedRuntimeTransportField(raw []byte) error {
	var document struct {
		Runtime *struct {
			Transport json.RawMessage `json:"transport"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		// Malformed JSON is reported by the real decode path below.
		return nil
	}
	if document.Runtime != nil && document.Runtime.Transport != nil {
		return fmt.Errorf("bootstrap config field runtime.transport has been removed: the runtime.transport section (maxIdleConns, maxIdleConnsPerHost, maxConnsPerHost, requestTimeout, idleConnTimeout, responseHeaderTimeout, tlsHandshakeTimeout, expectContinueTimeout) no longer exists in this version; outbound provider requests are no longer limited. Delete the runtime.transport section from the config file before upgrading")
	}
	return nil
}

func hasBootstrapField(value json.RawMessage) bool {
	return len(bytes.TrimSpace(value)) > 0
}

type bootstrapUnsupportedFieldProbe struct {
	SecretPayload json.RawMessage            `json:"secretPayload"`
	Database      map[string]json.RawMessage `json:"database"`
	Auth          map[string]json.RawMessage `json:"auth"`
}

func detectUnsupportedBootstrapFormat(raw []byte) error {
	var probe bootstrapUnsupportedFieldProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	unsupportedFields := make([]string, 0, 4)
	if hasBootstrapField(probe.SecretPayload) {
		unsupportedFields = append(unsupportedFields, "secretPayload")
	}
	if hasBootstrapMapField(probe.Database, "urlSecretRef") {
		unsupportedFields = append(unsupportedFields, "database.urlSecretRef")
	}
	if hasBootstrapMapField(probe.Auth, "jwtSigningKeySecretRef") {
		unsupportedFields = append(unsupportedFields, "auth.jwtSigningKeySecretRef")
	}
	if len(unsupportedFields) == 0 {
		return nil
	}
	return fmt.Errorf("bootstrap config uses unsupported legacy encrypted format fields: %s", strings.Join(unsupportedFields, ", "))
}

func hasBootstrapMapField(fields map[string]json.RawMessage, name string) bool {
	if fields == nil {
		return false
	}
	value, ok := fields[name]
	if !ok {
		return false
	}
	return hasBootstrapField(value)
}
