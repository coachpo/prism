package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactImageResponseAuditBodyKeepsMetadataAndDropsPayload(t *testing.T) {
	body := []byte(`{"created":1713833628,"data":[{"b64_json":"AAAABBBBCCCC"}],"size":"1024x1024","quality":"high","usage":{"total_tokens":100,"input_tokens":50,"output_tokens":50,"input_tokens_details":{"text_tokens":10,"image_tokens":40}}}`)

	redacted := RedactImageResponseAuditBody(body, "application/json")
	if strings.Contains(string(redacted), "AAAABBBBCCCC") {
		t.Fatalf("expected base64 payload to be removed, got %s", redacted)
	}

	var payload map[string]any
	if err := json.Unmarshal(redacted, &payload); err != nil {
		t.Fatalf("expected redacted response to stay valid JSON: %v", err)
	}
	if payload["size"] != "1024x1024" || payload["quality"] != "high" {
		t.Fatalf("expected image metadata to survive redaction, got %s", redacted)
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage object to survive redaction, got %s", redacted)
	}
	if usage["total_tokens"].(float64) != 100 {
		t.Fatalf("expected usage totals to survive redaction, got %s", redacted)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected one data entry, got %s", redacted)
	}
	entry := data[0].(map[string]any)
	if entry["b64_json"] != redactedImagePayload {
		t.Fatalf("expected b64_json to be redacted, got %s", redacted)
	}
}

func TestRedactImageRequestAuditBodyRedactsInlineDataURLsOnly(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"a cat","images":[{"image_url":"data:image/png;base64,SECRETPAYLOAD"},{"image_url":"https://example.com/a.png"},{"file_id":"file-123"}],"mask":{"image_url":"data:image/png;base64,MASKPAYLOAD"}}`)

	redacted := RedactImageRequestAuditBody(body)
	text := string(redacted)
	if strings.Contains(text, "SECRETPAYLOAD") || strings.Contains(text, "MASKPAYLOAD") {
		t.Fatalf("expected inline data URLs to be redacted, got %s", text)
	}
	if !strings.Contains(text, "https://example.com/a.png") {
		t.Fatalf("expected plain image URLs to survive, got %s", text)
	}
	if !strings.Contains(text, "file-123") {
		t.Fatalf("expected uploaded file ids to survive, got %s", text)
	}
	if !strings.Contains(text, "a cat") {
		t.Fatalf("expected the prompt to survive, got %s", text)
	}
}

// `mask` is redacted wholesale because the multipart contract puts raw bytes
// there; the JSON contract nests a reference under it, which the recursive walk
// reaches through the object instead.
func TestRedactImageRequestAuditBodyRedactsRawByteFields(t *testing.T) {
	redacted := string(RedactImageRequestAuditBody([]byte(`{"image":"RAWBYTES","mask":"RAWMASK","prompt":"keep"}`)))
	if strings.Contains(redacted, "RAWBYTES") || strings.Contains(redacted, "RAWMASK") {
		t.Fatalf("expected raw byte fields to be redacted, got %s", redacted)
	}
	if !strings.Contains(redacted, "keep") {
		t.Fatalf("expected prompt to survive, got %s", redacted)
	}
}

func TestRedactImageResponseAuditBodyRedactsEventStreamPerEvent(t *testing.T) {
	stream := "event: image_generation.partial_image\n" +
		`data: {"type":"image_generation.partial_image","b64_json":"PARTIALPAYLOAD","partial_image_index":0}` + "\n" +
		"\n" +
		"event: image_generation.completed\n" +
		`data: {"type":"image_generation.completed","b64_json":"FINALPAYLOAD","usage":{"total_tokens":100,"input_tokens":50,"output_tokens":50}}` + "\n" +
		"\n"

	redacted := string(RedactImageResponseAuditBody([]byte(stream), "text/event-stream"))
	if strings.Contains(redacted, "PARTIALPAYLOAD") || strings.Contains(redacted, "FINALPAYLOAD") {
		t.Fatalf("expected every event payload to be redacted, got %s", redacted)
	}
	if !strings.Contains(redacted, "event: image_generation.partial_image") {
		t.Fatalf("expected event frames to survive, got %s", redacted)
	}
	if !strings.Contains(redacted, "event: image_generation.completed") {
		t.Fatalf("expected the terminal event frame to survive, got %s", redacted)
	}
	if !strings.Contains(redacted, `"partial_image_index":0`) {
		t.Fatalf("expected partial image indices to survive, got %s", redacted)
	}
	if !strings.Contains(redacted, `"total_tokens":100`) {
		t.Fatalf("expected terminal usage to survive, got %s", redacted)
	}
}

// A stream truncated mid-event leaves an unparseable final data line. It must
// be replaced wholesale rather than passed through with a partial blob.
func TestRedactImageResponseAuditBodyReplacesTruncatedEventPayload(t *testing.T) {
	stream := "event: image_generation.completed\n" +
		`data: {"type":"image_generation.completed","b64_json":"TRUNCATEDPAYLO`

	redacted := string(RedactImageResponseAuditBody([]byte(stream), "text/event-stream"))
	if strings.Contains(redacted, "TRUNCATEDPAYLO") {
		t.Fatalf("expected a truncated payload to be dropped, got %s", redacted)
	}
	if !strings.Contains(redacted, redactedImageResponse) {
		t.Fatalf("expected the fallback placeholder, got %s", redacted)
	}
}

func TestRedactImageResponseAuditBodyReplacesUnparseableJSON(t *testing.T) {
	redacted := string(RedactImageResponseAuditBody([]byte(`{"data":[{"b64_json":"TRUNC`), "application/json"))
	if strings.Contains(redacted, "TRUNC") {
		t.Fatalf("expected a truncated JSON body to be dropped, got %s", redacted)
	}
	if redacted != redactedImageResponse {
		t.Fatalf("expected the fallback placeholder, got %s", redacted)
	}
}

func TestRedactImageAuditBodiesIgnoreEmptyInput(t *testing.T) {
	if got := RedactImageRequestAuditBody(nil); got != nil {
		t.Fatalf("expected nil for an empty request body, got %s", got)
	}
	if got := RedactImageResponseAuditBody([]byte("   "), "application/json"); got != nil {
		t.Fatalf("expected nil for a blank response body, got %s", got)
	}
}

func TestRedactImageEventStreamKeepsDoneSentinel(t *testing.T) {
	redacted := string(RedactImageResponseAuditBody([]byte("data: [DONE]\n"), "text/event-stream"))
	if !strings.Contains(redacted, "data: [DONE]") {
		t.Fatalf("expected the DONE sentinel to survive, got %s", redacted)
	}
}
