package configbundle

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProfileBundleV3ConnectionAliasesStayConnectionShaped(t *testing.T) {
	request := validProfileBundleV3Request()
	bundle := profileBundleResponse{
		Version:               request.Version,
		BundleKind:            request.BundleKind,
		VendorRefs:            request.VendorRefs,
		Endpoints:             request.Endpoints,
		PricingTemplates:      request.PricingTemplates,
		Connections:           request.Connections,
		LoadbalanceStrategies: request.LoadbalanceStrategies,
		Models:                request.Models,
		ProfileSettings:       *request.ProfileSettings,
		HeaderBlocklistRules:  request.HeaderBlocklistRules,
		UserAgentClientRules:  request.UserAgentClientRules,
		SecretPayload:         request.SecretPayload,
	}

	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal profile bundle: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal profile bundle envelope: %v", err)
	}
	if _, ok := envelope["connections"]; !ok {
		t.Fatalf("expected top-level connections key in v3 bundle: %s", raw)
	}
	if _, ok := envelope["terminal_targets"]; ok {
		t.Fatalf("v3 bundle must not expose terminal_targets alias: %s", raw)
	}

	var connections []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["connections"], &connections); err != nil {
		t.Fatalf("unmarshal connections: %v", err)
	}
	if len(connections) != 1 {
		t.Fatalf("expected one top-level connection, got %+v", connections)
	}
	if _, ok := connections[0]["ref"]; !ok {
		t.Fatalf("expected connection ref field to stay named ref, got %+v", connections[0])
	}

	var models []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		t.Fatalf("unmarshal models: %v", err)
	}
	var accessTargets []map[string]json.RawMessage
	if err := json.Unmarshal(models[0]["access_targets"], &accessTargets); err != nil {
		t.Fatalf("unmarshal access targets: %v", err)
	}
	if _, ok := accessTargets[0]["connection_ref"]; !ok {
		t.Fatalf("expected access target connection_ref, got %+v", accessTargets[0])
	}
	if _, ok := accessTargets[0]["terminal_target_ref"]; ok {
		t.Fatalf("v3 access target must not expose terminal_target_ref alias: %+v", accessTargets[0])
	}

	var imported profileImportRequest
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatalf("unmarshal imported bundle: %v", err)
	}
	if err := validateProfileImportRequest(imported); err != nil {
		t.Fatalf("validate imported bundle: %v", err)
	}
}

func TestProfileBundlePreviewRejectsTerminalTargetAliasKeys(t *testing.T) {
	raw := []byte(`{"version":3,"bundle_kind":"profile_config","terminal_targets":[]}`)
	service := &Service{}
	response := httptest.NewRecorder()
	service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, "/api/config/profile/import/preview", bytes.NewReader(raw)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected terminal_targets alias to reject with 400, got status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["detail"] != `json: unknown field "terminal_targets"` {
		t.Fatalf("unexpected terminal_targets rejection detail: %q", body["detail"])
	}
}
