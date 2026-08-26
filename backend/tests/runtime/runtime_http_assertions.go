package runtimetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func (h *runtimeHarness) requestJSON(tb testing.TB, method string, path string, body any, headers map[string]string) *http.Response {
	tb.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			tb.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		tb.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		tb.Fatalf("perform request %s %s: %v", method, path, err)
	}
	tb.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

func assertStatus(tb testing.TB, response *http.Response, want int) {
	tb.Helper()
	if response.StatusCode != want {
		body := readResponseBody(tb, response)
		tb.Fatalf("expected status %d, got %d with body %s", want, response.StatusCode, body)
	}
}

func assertResponseField(t *testing.T, response *http.Response, field string, want string) {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if got, _ := payload[field].(string); got != want {
		t.Fatalf("expected response field %q=%q, got %+v", field, want, payload)
	}
}

func decodeJSONResponse(tb testing.TB, response *http.Response, target any) {
	tb.Helper()
	body := readResponseBody(tb, response)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		tb.Fatalf("decode response JSON %q: %v", body, err)
	}
}

// decodeConnectionMutationConnection unwraps the {connection, access_targets,
// configuration_warnings} envelope returned by owner-scoped connection
// create/update mutations.
func decodeConnectionMutationConnection(tb testing.TB, response *http.Response) map[string]any {
	tb.Helper()
	var envelope map[string]any
	decodeJSONResponse(tb, response, &envelope)
	connection, ok := envelope["connection"].(map[string]any)
	if !ok {
		tb.Fatalf("expected connection mutation envelope to include connection, got %+v", envelope)
	}
	return connection
}

func readResponseBody(tb testing.TB, response *http.Response) string {
	tb.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		tb.Fatalf("read response body: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return strings.TrimSpace(string(raw))
}

type concurrentRuntimeRequestResult struct {
	StatusCode int
	Body       string
	Err        error
}

type persistedRuntimeState struct {
	CycleRetryAttempts                  int
	CumulativeRetryAttempts             int
	NextRetryAt                         sql.NullTime
	LastRetryDelayMS                    int
	LastFailureKind                     sql.NullString
	BanMode                             string
	BannedUntilAt                       sql.NullTime
	LastSuccessAt                       sql.NullTime
	WindowRequestCount                  int
	InFlightNonStream                   int
	InFlightStream                      int
	LastSuccessResponseHeadersLatencyMS sql.NullInt32
	ConsecutiveFailures                 int
	LastCooldownSeconds                 float64
	MaxCooldownStrikes                  int
	OpenUntilAt                         sql.NullTime
	ProbeEligibleLogged                 bool
	CircuitState                        string
	ProbeAvailableAt                    sql.NullTime
	LastLiveFailureKind                 sql.NullString
	LastLiveFailureAt                   sql.NullTime
	LastLiveSuccessAt                   sql.NullTime
}

type persistedLoadbalanceEvent struct {
	EventType               string
	FailureKind             sql.NullString
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             sql.NullTime
	LastRetryDelayMS        int
	ModelID                 sql.NullString
	EndpointID              sql.NullInt32
	BanMode                 sql.NullString
	BannedUntilAt           sql.NullTime
	LastSuccessAt           sql.NullTime
	ConsecutiveFailures     int
	CooldownSeconds         float64
}

func runtimeStateExists(t *testing.T, harness *runtimeHarness, profileID int, connectionID int) bool {
	t.Helper()
	_, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	return ok
}

func loadRuntimeState(t *testing.T, harness *runtimeHarness, profileID int, connectionID int) persistedRuntimeState {
	t.Helper()
	snapshot, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if !ok {
		t.Fatalf("load runtime state for connection %d: missing local runtime state", connectionID)
	}
	circuitState := "closed"
	if snapshot.NextRetryAt != nil || snapshot.CycleRetryAttempts > 0 {
		circuitState = "open"
	}
	return persistedRuntimeState{
		CycleRetryAttempts:                  snapshot.CycleRetryAttempts,
		CumulativeRetryAttempts:             snapshot.CumulativeRetryAttempts,
		NextRetryAt:                         sqlNullTime(snapshot.NextRetryAt),
		LastRetryDelayMS:                    snapshot.LastRetryDelayMS,
		LastFailureKind:                     sqlNullString(snapshot.LastFailureKind),
		BanMode:                             snapshot.BanMode,
		BannedUntilAt:                       sqlNullTime(snapshot.BannedUntilAt),
		LastSuccessAt:                       sqlNullTime(snapshot.LastSuccessAt),
		WindowRequestCount:                  snapshot.WindowRequestCount,
		InFlightNonStream:                   snapshot.InFlightNonStream,
		InFlightStream:                      snapshot.InFlightStream,
		LastSuccessResponseHeadersLatencyMS: sqlNullInt32(snapshot.LastSuccessResponseHeadersLatencyMS),
		ConsecutiveFailures:                 snapshot.CumulativeRetryAttempts,
		LastCooldownSeconds:                 float64(snapshot.LastRetryDelayMS) / 1000,
		MaxCooldownStrikes:                  snapshot.CycleRetryAttempts,
		OpenUntilAt:                         sqlNullTime(snapshot.NextRetryAt),
		CircuitState:                        circuitState,
		ProbeAvailableAt:                    sqlNullTime(snapshot.NextRetryAt),
		LastLiveFailureKind:                 sqlNullString(snapshot.LastFailureKind),
		LastLiveSuccessAt:                   sqlNullTime(snapshot.LastSuccessAt),
	}
}

func loadLoadbalanceEvents(t *testing.T, conn *pgx.Conn, profileID int, connectionID int) []persistedLoadbalanceEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last []persistedLoadbalanceEvent
	stableReads := 0
	for {
		last = queryLoadbalanceEvents(t, conn, profileID, connectionID)
		if len(last) > 0 {
			stableReads++
			if stableReads >= 3 {
				return last
			}
		} else {
			stableReads = 0
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryLoadbalanceEvents(t *testing.T, conn *pgx.Conn, profileID int, connectionID int) []persistedLoadbalanceEvent {
	t.Helper()
	rows, err := conn.Query(
		context.Background(),
		`SELECT event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, banned_until_at, last_success_at
		FROM loadbalance_events
		WHERE profile_id = $1 AND connection_id = $2
		ORDER BY created_at ASC, id ASC`,
		profileID,
		connectionID,
	)
	if err != nil {
		t.Fatalf("query loadbalance events for connection %d: %v", connectionID, err)
	}
	defer rows.Close()
	events := make([]persistedLoadbalanceEvent, 0)
	for rows.Next() {
		item := persistedLoadbalanceEvent{}
		if err := rows.Scan(&item.EventType, &item.FailureKind, &item.CycleRetryAttempts, &item.CumulativeRetryAttempts, &item.NextRetryAt, &item.LastRetryDelayMS, &item.ModelID, &item.EndpointID, &item.BanMode, &item.BannedUntilAt, &item.LastSuccessAt); err != nil {
			t.Fatalf("scan loadbalance event for connection %d: %v", connectionID, err)
		}
		item.ConsecutiveFailures = item.CumulativeRetryAttempts
		item.CooldownSeconds = float64(item.LastRetryDelayMS) / 1000
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate loadbalance events for connection %d: %v", connectionID, err)
	}
	return events
}

func assertLoadbalanceEventTypeSequence(t *testing.T, events []persistedLoadbalanceEvent, want ...string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("expected %d loadbalance events %v, got %+v", len(want), want, events)
	}
	for index, eventType := range want {
		if events[index].EventType != eventType {
			t.Fatalf("expected loadbalance event %d to be %q, got %+v", index, eventType, events[index])
		}
	}
}

func loadRoundRobinNextCursor(t *testing.T, harness *runtimeHarness, profileID int, modelConfigID int, connectionCount int) int {
	t.Helper()
	return harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, connectionCount)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func sqlNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func sqlNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func sqlNullInt32(value *int) sql.NullInt32 {
	if value == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*value), Valid: true}
}

func (h *runtimeHarness) modelConfigIDForConnection(tb testing.TB, connectionID int) int {
	tb.Helper()
	var modelConfigID int
	if err := h.conn.QueryRow(context.Background(), `SELECT source_model_config_id FROM model_access_targets WHERE target_connection_id = $1 ORDER BY source_model_config_id ASC LIMIT 1`, connectionID).Scan(&modelConfigID); err != nil {
		tb.Fatalf("load model config id for connection %d: %v", connectionID, err)
	}
	return modelConfigID
}

func requestModelID(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode upstream request body: %v", err)
	}
	modelID, _ := payload["model"].(string)
	return modelID
}

func marshalNullableJSON(tb testing.TB, value any) any {
	tb.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		tb.Fatalf("marshal JSON value: %v", err)
	}
	return string(raw)
}

func nullableTestInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func runtimeStringPtr(value string) *string {
	return &value
}

func jsonInt(tb testing.TB, value any) int {
	tb.Helper()
	floatValue, ok := value.(float64)
	if !ok {
		tb.Fatalf("expected JSON number, got %T", value)
	}
	return int(floatValue)
}

func executeConcurrentRuntimeJSONRequests(t *testing.T, harness *runtimeHarness, requestCount int, method string, path string, body any, headers map[string]string) []concurrentRuntimeRequestResult {
	t.Helper()
	if requestCount < 1 {
		t.Fatalf("concurrent request count must be >= 1, got %d", requestCount)
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal concurrent request body: %v", err)
	}
	results := make([]concurrentRuntimeRequestResult, requestCount)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < requestCount; index++ {
		wg.Add(1)
		go func(resultIndex int) {
			defer wg.Done()
			<-start
			request, requestErr := http.NewRequest(method, harness.url+path, bytes.NewReader(rawBody))
			if requestErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("build request %s %s: %w", method, path, requestErr)}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			response, responseErr := harness.client.Do(request)
			if responseErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("perform request %s %s: %w", method, path, responseErr)}
				return
			}
			defer func() { _ = response.Body.Close() }()
			responseBody, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				results[resultIndex] = concurrentRuntimeRequestResult{Err: fmt.Errorf("read response body: %w", readErr)}
				return
			}
			results[resultIndex] = concurrentRuntimeRequestResult{
				StatusCode: response.StatusCode,
				Body:       strings.TrimSpace(string(responseBody)),
			}
		}(index)
	}
	close(start)
	wg.Wait()
	return results
}
