package contracttest

import (
	"fmt"
	"net/http"
	"testing"
)

func routingScheduleBody(timezone string, windows ...map[string]any) map[string]any {
	return map[string]any{"timezone": timezone, "windows": windows}
}

func routingWindow(mask, start, end int) map[string]any {
	return map[string]any{"weekday_mask": mask, "start_minute": start, "end_minute": end}
}

// TestConnectionRoutingScheduleWriteContract covers the create/PATCH three-state
// semantics, the validation status matrix, window replacement, copy
// propagation, and the requirement that every surface exposing a connection
// renders routing_schedule and routing_schedule_state identically.
func TestConnectionRoutingScheduleWriteContract(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Routing Schedule Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "routing-schedule-owner", nil, "native", &strategyID, true)
	copyTargetModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "routing-schedule-copy-target", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Routing Schedule Endpoint")
	createPath := fmt.Sprintf("/api/models/%d/connections", ownerModelID)
	base := func(extra map[string]any) map[string]any {
		body := map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native"}
		for key, value := range extra {
			body[key] = value
		}
		return body
	}

	t.Run("create three-state semantics", func(t *testing.T) {
		for _, testCase := range []struct {
			name          string
			body          map[string]any
			wantConfigued bool
		}{
			{name: "missing field is unconfigured", body: base(nil)},
			{name: "null field is unconfigured", body: base(map[string]any{"routing_schedule": nil})},
			{
				name:          "object is stored",
				body:          base(map[string]any{"routing_schedule": routingScheduleBody("Asia/Shanghai", routingWindow(31, 540, 1080))}),
				wantConfigued: true,
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				response := harness.requestJSON(t, harness.client, http.MethodPost, createPath, testCase.body, modelHeader(defaultProfileID))
				assertStatus(t, response, http.StatusCreated)
				payload := connectionMutationConnection(t, response)
				schedule, present := payload["routing_schedule"]
				if !present {
					t.Fatal("expected routing_schedule key to always be present")
				}
				state, statePresent := payload["routing_schedule_state"]
				if !statePresent {
					t.Fatal("expected routing_schedule_state key to always be present")
				}
				if !testCase.wantConfigued {
					// Unconfigured must render as null on both keys: an empty
					// object would read as "configured with no windows", and a
					// fabricated state would claim a conclusion never computed.
					if schedule != nil || state != nil {
						t.Fatalf("expected both keys null when unconfigured, got schedule=%v state=%v", schedule, state)
					}
					return
				}
				stored := asMap(t, schedule)
				if stored["timezone"] != "Asia/Shanghai" {
					t.Fatalf("unexpected timezone round-trip: %v", stored["timezone"])
				}
				windows := asSliceOfMaps(t, stored["windows"])
				if len(windows) != 1 || jsonInt(t, windows[0]["weekday_mask"]) != 31 || jsonInt(t, windows[0]["start_minute"]) != 540 || jsonInt(t, windows[0]["end_minute"]) != 1080 {
					t.Fatalf("unexpected window round-trip: %+v", windows)
				}
				evaluated := asMap(t, state)
				switch evaluated["status"] {
				case "open", "closed":
				default:
					t.Fatalf("expected an evaluated open/closed status, got %v", evaluated["status"])
				}
			})
		}
	})

	t.Run("validation status matrix", func(t *testing.T) {
		for _, testCase := range []struct {
			name       string
			schedule   any
			wantStatus int
			wantReason string
		}{
			{name: "no windows", schedule: routingScheduleBody("Asia/Shanghai"), wantStatus: http.StatusUnprocessableEntity, wantReason: "no_windows"},
			{name: "missing timezone", schedule: routingScheduleBody("", routingWindow(31, 540, 1080)), wantStatus: http.StatusUnprocessableEntity, wantReason: "timezone_required"},
			{name: "unknown timezone", schedule: routingScheduleBody("Mars/Olympus", routingWindow(31, 540, 1080)), wantStatus: http.StatusUnprocessableEntity, wantReason: "timezone_unknown"},
			{name: "local timezone is refused", schedule: routingScheduleBody("Local", routingWindow(31, 540, 1080)), wantStatus: http.StatusUnprocessableEntity, wantReason: "timezone_not_allowed"},
			{name: "weekday mask out of range", schedule: routingScheduleBody("Asia/Shanghai", routingWindow(384, 540, 1080)), wantStatus: http.StatusUnprocessableEntity, wantReason: "weekday_mask_out_of_range"},
			{name: "end not after start", schedule: routingScheduleBody("Asia/Shanghai", routingWindow(31, 600, 600)), wantStatus: http.StatusUnprocessableEntity, wantReason: "end_minute_not_after_start"},
			{name: "span exceeds one day", schedule: routingScheduleBody("Asia/Shanghai", routingWindow(31, 0, 1441)), wantStatus: http.StatusUnprocessableEntity, wantReason: "span_exceeds_one_day"},
			{name: "duplicate window", schedule: routingScheduleBody("Asia/Shanghai", routingWindow(31, 540, 1080), routingWindow(31, 540, 1080)), wantStatus: http.StatusUnprocessableEntity, wantReason: "duplicate_window"},
			{name: "full week coverage is refused", schedule: routingScheduleBody("Asia/Shanghai", routingWindow(127, 0, 1440)), wantStatus: http.StatusUnprocessableEntity, wantReason: "covers_full_week"},
			{name: "unknown key is refused", schedule: map[string]any{"timezone": "Asia/Shanghai", "windows": []map[string]any{routingWindow(31, 540, 1080)}, "enabled": true}, wantStatus: http.StatusUnprocessableEntity, wantReason: "malformed"},
			{name: "array root is refused", schedule: []any{}, wantStatus: http.StatusUnprocessableEntity, wantReason: "malformed"},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				response := harness.requestJSON(t, harness.client, http.MethodPost, createPath, base(map[string]any{"routing_schedule": testCase.schedule}), modelHeader(defaultProfileID))
				assertStatus(t, response, testCase.wantStatus)
				var payload map[string]any
				decodeJSONResponse(t, response, &payload)
				if payload["reason"] != testCase.wantReason {
					t.Fatalf("expected reason %q, got %v (body %+v)", testCase.wantReason, payload["reason"], payload)
				}
				if payload["field"] != "routing_schedule" {
					t.Fatalf("expected field routing_schedule, got %v", payload["field"])
				}
			})
		}
	})

	// An over-length timezone is a 400 rather than a 422, matching the split
	// already used by the settings timezone preference validator.
	t.Run("over-length timezone is a bad request", func(t *testing.T) {
		long := ""
		for len(long) <= 100 {
			long += "Asia/Shanghai"
		}
		response := harness.requestJSON(t, harness.client, http.MethodPost, createPath, base(map[string]any{"routing_schedule": routingScheduleBody(long, routingWindow(31, 540, 1080))}), modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusBadRequest)
	})

	t.Run("patch three-state semantics and shape parity", func(t *testing.T) {
		created := harness.requestJSON(t, harness.client, http.MethodPost, createPath,
			base(map[string]any{"routing_schedule": routingScheduleBody("Asia/Shanghai", routingWindow(31, 540, 1080))}), modelHeader(defaultProfileID))
		assertStatus(t, created, http.StatusCreated)
		connectionID := jsonInt(t, connectionMutationConnection(t, created)["id"])
		patchPath := fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID)

		renamed := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"name": "renamed"}, modelHeader(defaultProfileID))
		assertStatus(t, renamed, http.StatusOK)
		kept := asMap(t, connectionMutationConnection(t, renamed)["routing_schedule"])
		if len(asSliceOfMaps(t, kept["windows"])) != 1 || kept["timezone"] != "Asia/Shanghai" {
			t.Fatalf("a PATCH that omits routing_schedule must leave it untouched, got %+v", kept)
		}

		replaced := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath,
			map[string]any{"routing_schedule": routingScheduleBody("Europe/Berlin", routingWindow(96, 1320, 1800), routingWindow(1, 0, 300))}, modelHeader(defaultProfileID))
		assertStatus(t, replaced, http.StatusOK)
		next := asMap(t, connectionMutationConnection(t, replaced)["routing_schedule"])
		if next["timezone"] != "Europe/Berlin" || len(asSliceOfMaps(t, next["windows"])) != 2 {
			t.Fatalf("an object PATCH must replace the whole field, got %+v", next)
		}

		// The connection must render identically wherever it is exposed: the
		// model detail page reads the same TS type from more than one key, so a
		// shape that differs per surface degrades silently on one of them.
		listed := harness.requestJSON(t, harness.client, http.MethodGet, "/api/connections", nil, modelHeader(defaultProfileID))
		assertStatus(t, listed, http.StatusOK)
		var listPayload []map[string]any
		decodeJSONResponse(t, listed, &listPayload)
		found := false
		for _, item := range listPayload {
			if jsonInt(t, item["id"]) != connectionID {
				continue
			}
			found = true
			listed := asMap(t, item["routing_schedule"])
			if listed["timezone"] != next["timezone"] || len(asSliceOfMaps(t, listed["windows"])) != 2 {
				t.Fatalf("list and mutation surfaces disagree: %+v vs %+v", listed, next)
			}
			if _, present := item["routing_schedule_state"]; !present {
				t.Fatal("expected routing_schedule_state on the list surface too")
			}
		}
		if !found {
			t.Fatalf("connection %d missing from /api/connections", connectionID)
		}

		cleared := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"routing_schedule": nil}, modelHeader(defaultProfileID))
		assertStatus(t, cleared, http.StatusOK)
		clearedPayload := connectionMutationConnection(t, cleared)
		if clearedPayload["routing_schedule"] != nil || clearedPayload["routing_schedule_state"] != nil {
			t.Fatalf("null must clear both keys, got %+v / %+v", clearedPayload["routing_schedule"], clearedPayload["routing_schedule_state"])
		}
	})

	t.Run("copies carry the window rows", func(t *testing.T) {
		created := harness.requestJSON(t, harness.client, http.MethodPost, createPath,
			base(map[string]any{"routing_schedule": routingScheduleBody("Asia/Shanghai", routingWindow(31, 540, 1080))}), modelHeader(defaultProfileID))
		assertStatus(t, created, http.StatusCreated)
		sourceID := jsonInt(t, connectionMutationConnection(t, created)["id"])

		copied := harness.requestJSON(t, harness.client, http.MethodPost,
			fmt.Sprintf("/api/models/%d/connections/%d/copies", ownerModelID, sourceID),
			map[string]any{"destination_model_config_ids": []int{copyTargetModelID}, "enable_copies": true}, modelHeader(defaultProfileID))
		assertStatus(t, copied, http.StatusCreated)

		targetConnections := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", copyTargetModelID), nil, modelHeader(defaultProfileID))
		assertStatus(t, targetConnections, http.StatusOK)
		var items []map[string]any
		decodeJSONResponse(t, targetConnections, &items)
		if len(items) != 1 {
			t.Fatalf("expected exactly one copied connection, got %d", len(items))
		}
		// The connectionResponse literal clone cannot carry child rows, so a
		// copy that forgets the explicit window copy lands with a timezone and
		// zero windows while every other assertion still passes.
		schedule := asMap(t, items[0]["routing_schedule"])
		if schedule["timezone"] != "Asia/Shanghai" || len(asSliceOfMaps(t, schedule["windows"])) != 1 {
			t.Fatalf("copy dropped the routing windows: %+v", schedule)
		}
	})
}
