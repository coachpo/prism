package runtimetest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Image streams are opted into by the request body's `stream` flag and end on a
// single terminal event that carries the usage object. Partial-image events
// carry no usage and must not terminate the stream.
func TestOpenAIImageStreamsCaptureTerminalUsage(t *testing.T) {
	tests := []struct {
		name           string
		slug           string
		requestPath    string
		requestBody    func(publicModelID string) any
		streamBody     string
		wantInput      int64
		wantOutput     int64
		wantTotal      int64
		imageDimension string
	}{
		{
			name:        "Generations",
			slug:        "openai-images-generations-stream",
			requestPath: "/v1/images/generations",
			requestBody: func(publicModelID string) any {
				return map[string]any{"model": publicModelID, "prompt": "streaming image", "stream": true, "partial_images": 1}
			},
			streamBody: "event: image_generation.partial_image\n" +
				`data: {"type":"image_generation.partial_image","b64_json":"cGFydGlhbA==","partial_image_index":0}` + "\n\n" +
				"event: image_generation.completed\n" +
				`data: {"type":"image_generation.completed","b64_json":"ZmluYWw=","created_at":1713833628,"size":"1024x1024","usage":{"input_tokens":11,"output_tokens":41,"total_tokens":52}}` + "\n\n",
			wantInput:      11,
			wantOutput:     41,
			wantTotal:      52,
			imageDimension: "generations",
		},
		{
			name:        "Edits",
			slug:        "openai-images-edits-stream",
			requestPath: "/v1/images/edits",
			requestBody: func(publicModelID string) any {
				return map[string]any{"model": publicModelID, "prompt": "streaming edit", "images": []map[string]any{{"file_id": "file-stream"}}, "stream": true}
			},
			streamBody: "event: image_edit.partial_image\n" +
				`data: {"type":"image_edit.partial_image","b64_json":"cGFydGlhbA==","partial_image_index":0}` + "\n\n" +
				"event: image_edit.completed\n" +
				`data: {"type":"image_edit.completed","b64_json":"ZmluYWw=","created_at":1713833629,"size":"1024x1024","usage":{"input_tokens":13,"output_tokens":29,"total_tokens":42}}` + "\n\n",
			wantInput:      13,
			wantOutput:     29,
			wantTotal:      42,
			imageDimension: "edits",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte(test.streamBody))
			endpointPrefix := "/" + test.slug
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:             profileID,
				APIFamily:             "openai",
				PublicModelID:         "image-stream-public-" + test.slug + "-" + randomSuffix(),
				TargetModelID:         "image-stream-target-" + test.slug + "-" + randomSuffix(),
				EndpointBaseURL:       upstream.baseURL(endpointPrefix),
				EndpointAPIKey:        "image-stream-key-" + test.slug,
				OpenAIImageOperations: runtimeStringPtr(test.imageDimension),
			})

			response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route.PublicModelID), nil)
			assertStatus(t, response, http.StatusOK)
			body := readResponseBody(t, response)
			if !strings.Contains(body, "partial_image") {
				t.Fatalf("expected partial image events to pass through, got %q", body)
			}
			if !strings.Contains(body, ".completed") {
				t.Fatalf("expected the terminal event to pass through, got %q", body)
			}

			upstreamRequest := upstream.lastRequest(t)
			if upstreamRequest.Path != endpointPrefix+test.requestPath {
				t.Fatalf("expected upstream path %q, got %q", endpointPrefix+test.requestPath, upstreamRequest.Path)
			}
			// The model is rewritten to the target even on the streaming path.
			if !strings.Contains(string(upstreamRequest.Body), route.TargetModelID) {
				t.Fatalf("expected upstream body to carry the target model, got %s", upstreamRequest.Body)
			}

			assertRouteMatrixUsage(t, harness, profileID, routeMatrixUsageExpectation{
				isStream:      true,
				streamOutcome: "completed",
				inputTokens:   routeMatrixInt64(test.wantInput),
				outputTokens:  routeMatrixInt64(test.wantOutput),
				totalTokens:   routeMatrixInt64(test.wantTotal),
			})
		})
	}
}

// A model that only accepts generations must reject an edits request before any
// upstream call, proving the image gate is per-operation rather than a single
// "supports images" flag.
func TestOpenAIImageGateRejectsUnacceptedOperation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"created":1,"data":[]}`))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:             profileID,
		APIFamily:             "openai",
		PublicModelID:         "image-gate-public-" + randomSuffix(),
		TargetModelID:         "image-gate-target-" + randomSuffix(),
		EndpointBaseURL:       upstream.baseURL("/image-gate"),
		EndpointAPIKey:        "image-gate-key",
		OpenAIImageOperations: runtimeStringPtr("generations"),
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/images/edits", map[string]any{
		"model":  route.PublicModelID,
		"prompt": "should be rejected",
		"images": []map[string]any{{"file_id": "file-rejected"}},
	}, nil)
	assertStatus(t, response, http.StatusBadRequest)
	if body := readResponseBody(t, response); !strings.Contains(body, "openai_operation_not_supported") {
		t.Fatalf("expected an unsupported-operation rejection, got %q", body)
	}
	if len(upstream.requestsSnapshot()) != 0 {
		t.Fatal("expected the image gate to reject before any upstream request")
	}
}

// A pure image model has no text mode, so a text request against it must be
// rejected rather than silently inheriting a text capability.
func TestOpenAIImageOnlyModelRejectsTextOperations(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"id":"unused"}`))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:             profileID,
		APIFamily:             "openai",
		PublicModelID:         "image-only-public-" + randomSuffix(),
		TargetModelID:         "image-only-target-" + randomSuffix(),
		EndpointBaseURL:       upstream.baseURL("/image-only"),
		EndpointAPIKey:        "image-only-key",
		OpenAIImageOperations: runtimeStringPtr("generations_and_edits"),
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":    route.PublicModelID,
		"messages": []map[string]any{{"role": "user", "content": "should be rejected"}},
	}, nil)
	assertStatus(t, response, http.StatusBadRequest)
	if len(upstream.requestsSnapshot()) != 0 {
		t.Fatal("expected a text request against an image-only model to reject before any upstream request")
	}
}

// Only the JSON edit channel is registered. A multipart body cannot yield a
// body-bound model, so it is rejected before transport rather than proxied
// unrouted.
func TestOpenAIImageEditsRejectMultipartBodies(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"created":1,"data":[]}`))
	harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:             profileID,
		APIFamily:             "openai",
		PublicModelID:         "image-multipart-public-" + randomSuffix(),
		TargetModelID:         "image-multipart-target-" + randomSuffix(),
		EndpointBaseURL:       upstream.baseURL("/image-multipart"),
		EndpointAPIKey:        "image-multipart-key",
		OpenAIImageOperations: runtimeStringPtr("edits"),
	})

	body := "--boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ngpt-image-2\r\n--boundary--\r\n"
	response := performRuntimeRawRequest(t, harness, http.MethodPost, "/v1/images/edits", []byte(body), "multipart/form-data; boundary=boundary")
	assertStatus(t, response, http.StatusBadRequest)
	if detail := readResponseBody(t, response); !strings.Contains(detail, "Cannot determine model for routing") {
		t.Fatalf("expected a routing-model rejection for a multipart edit body, got %q", detail)
	}
	if len(upstream.requestsSnapshot()) != 0 {
		t.Fatal("expected a multipart edit body to reject before any upstream request")
	}
}

// Base64 image payloads must never reach the audit tables. The surrounding
// metadata has to survive so an audited image call stays explainable.
func TestOpenAIImageAuditBodiesAreRedacted(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, true)

	upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"created":1713833628,"data":[{"b64_json":"U0VDUkVUSU1BR0VCWVRFUw=="}],"size":"1024x1024","usage":{"input_tokens":11,"output_tokens":41,"total_tokens":52}}`))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:             profileID,
		APIFamily:             "openai",
		PublicModelID:         "image-audit-public-" + randomSuffix(),
		TargetModelID:         "image-audit-target-" + randomSuffix(),
		EndpointBaseURL:       upstream.baseURL("/image-audit"),
		EndpointAPIKey:        "image-audit-key",
		OpenAIImageOperations: runtimeStringPtr("edits"),
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/images/edits", map[string]any{
		"model":  route.PublicModelID,
		"prompt": "audited edit",
		"images": []map[string]any{{"image_url": "data:image/png;base64,SU5MSU5FU0VDUkVU"}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	// The client still receives the real image; only the audit copy is redacted.
	if body := readResponseBody(t, response); !strings.Contains(body, "U0VDUkVUSU1BR0VCWVRFUw==") {
		t.Fatalf("expected the downstream response to carry the real image bytes, got %q", body)
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var requestBody, responseBody []byte
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT COALESCE(request_body, ''::bytea), COALESCE(response_body, ''::bytea) FROM audit_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&requestBody, &responseBody); err != nil {
		t.Fatalf("read audited image bodies: %v", err)
	}

	if strings.Contains(string(responseBody), "U0VDUkVUSU1BR0VCWVRFUw==") {
		t.Fatalf("expected the audited response body to drop the image payload, got %s", responseBody)
	}
	if !strings.Contains(string(responseBody), "1024x1024") {
		t.Fatalf("expected the audited response body to keep its metadata, got %s", responseBody)
	}
	if strings.Contains(string(requestBody), "SU5MSU5FU0VDUkVU") {
		t.Fatalf("expected the audited request body to drop the inline data URL, got %s", requestBody)
	}
	if !strings.Contains(string(requestBody), "audited edit") {
		t.Fatalf("expected the audited request body to keep the prompt, got %s", requestBody)
	}
}
