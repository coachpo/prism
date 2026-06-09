package core

import (
	"encoding/json"
	"testing"
)

func TestTypedErrorSerializationDeterministic(t *testing.T) {
	err := NewGatewayError(ErrorTypeRouting, "no_healthy_upstream", "No healthy upstream", 503,
		FieldError{Field: "route.rules[1]", Code: "unavailable", Detail: "route has no healthy candidates"},
		FieldError{Field: "model_id", Code: "required", Detail: "model id is required"},
	)
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal gateway error: %v", marshalErr)
	}
	want := `{"type":"routing","code":"no_healthy_upstream","detail":"No healthy upstream","status_code":503,"fields":[{"field":"model_id","code":"required","detail":"model id is required"},{"field":"route.rules[1]","code":"unavailable","detail":"route has no healthy candidates"}]}`
	if string(payload) != want {
		t.Fatalf("expected %s, got %s", want, string(payload))
	}
	if got := err.Error(); got != "no_healthy_upstream: No healthy upstream" {
		t.Fatalf("expected stable Error text, got %q", got)
	}
}

func TestConfigErrorUsesTypedEnvelope(t *testing.T) {
	err := NewConfigError("invalid_gateway_config", "Invalid gateway configuration",
		FieldError{Field: "upstreams[0].base_url", Code: "required", Detail: "base URL is required"},
	)
	if err.Type != ErrorTypeConfig || err.Code != "invalid_gateway_config" {
		t.Fatalf("expected config typed error, got %+v", err)
	}
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal config error: %v", marshalErr)
	}
	want := `{"type":"config","code":"invalid_gateway_config","detail":"Invalid gateway configuration","fields":[{"field":"upstreams[0].base_url","code":"required","detail":"base URL is required"}]}`
	if string(payload) != want {
		t.Fatalf("expected %s, got %s", want, string(payload))
	}
}
