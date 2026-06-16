package openai

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

type OpenAIErrorEnvelope struct {
	Message string
	Type    string
	Code    any
	Param   any
}

func normalizeUnsupportedReason(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "unsupported_request_shape"
}

func normalizeOpenAIErrorEnvelope(body any) OpenAIErrorEnvelope {
	if body == nil {
		return OpenAIErrorEnvelope{Message: "Upstream returned an empty error response", Type: "upstream_error"}
	}
	if text := strings.TrimSpace(stringValue(body)); text != "" {
		return OpenAIErrorEnvelope{Message: text, Type: "upstream_error"}
	}
	source, _ := body.(map[string]any)
	if nested, _ := source["error"].(map[string]any); nested != nil {
		source = nested
	}
	if source == nil {
		return OpenAIErrorEnvelope{Message: canonicalJSONString(body), Type: "upstream_error"}
	}
	message := firstNonEmptyString(source["message"], source["detail"], source["status_msg"])
	if message == "" {
		if base, _ := source["base_resp"].(map[string]any); base != nil {
			message = firstNonEmptyString(base["status_msg"])
		}
	}
	if message == "" {
		message = canonicalJSONString(source)
	}
	errorType := firstNonEmptyString(source["type"])
	if errorType == "" {
		errorType = "upstream_error"
	}
	code := source["code"]
	if code == nil {
		if base, _ := source["base_resp"].(map[string]any); base != nil {
			code = base["status_code"]
		}
	}
	return OpenAIErrorEnvelope{Message: message, Type: errorType, Code: code, Param: source["param"]}
}

func NormalizedOpenAIErrorObject(body any) map[string]any {
	envelope := normalizeOpenAIErrorEnvelope(body)
	return map[string]any{"error": map[string]any{"message": envelope.Message, "type": envelope.Type, "code": envelope.Code, "param": envelope.Param}}
}

func UnsupportedOpenAITranslationError(httpStatus int, code string, detail string, mode provider.TranslationMode, reason string) *provider.AdapterError {
	if httpStatus == 0 {
		httpStatus = http.StatusBadRequest
	}
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	fields["unsupported_reason"] = normalizeUnsupportedReason(reason)
	return &provider.AdapterError{HTTPStatus: httpStatus, Code: code, Detail: detail, Fields: fields}
}

func DecodeJSONMap(raw []byte) (map[string]any, error) {
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
