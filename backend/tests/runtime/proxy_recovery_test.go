package runtimetest

import (
	"net/http"
	"testing"
	"time"
)

func TestRuntimeLoadBalanceWinningSuccessUpdatesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-feedback-success",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/feedback/success"), priority: 0}},
	})
	expiredOpenUntil := time.Now().UTC().Add(-1 * time.Minute)
	staleFailureAt := time.Now().UTC().Add(-2 * time.Minute)
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:                           activeProfileID,
		ConnectionID:                        route.connectionIDs[0],
		BanMode:                             "off",
		BlockedUntilAt:                      &expiredOpenUntil,
		CircuitState:                        "open",
		LastSuccessResponseHeadersLatencyMS: &staleLatency,
		LastLiveFailureAt:                   &staleFailureAt,
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "feedback success mutation"), nil)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if successState.ConsecutiveFailures != 0 || successState.CircuitState != "closed" {
		t.Fatalf("expected success to reset recovery state, got %+v", successState)
	}
	if successState.OpenUntilAt.Valid || successState.ProbeAvailableAt.Valid {
		t.Fatalf("expected success to clear open/probe timers, got %+v", successState)
	}
	if !successState.LastLiveSuccessAt.Valid || !successState.LastSuccessResponseHeadersLatencyMS.Valid || successState.LastSuccessResponseHeadersLatencyMS.Int32 < 1 {
		t.Fatalf("expected success observation to persist latency and timestamp, got %+v", successState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, route.connectionIDs[0])
	assertLoadbalanceEventTypeSequence(t, events, "unbanned")
	if events[0].FailureKind.Valid {
		t.Fatalf("expected unbanned event after winning success to keep an empty failure kind for stale retry state, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != route.targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != route.endpointIDs[0] {
		t.Fatalf("expected unbanned event model/endpoint snapshot %q/%d, got %+v", route.targetModelID, route.endpointIDs[0], events[0])
	}
}

func TestRuntimeLoadBalanceProbeSuccessClosesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-probe-success",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/probe/success"), priority: 0}},
	})
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:                           activeProfileID,
		ConnectionID:                        route.connectionIDs[0],
		ConsecutiveFailures:                 1,
		LastFailureKind:                     &priorFailureKind,
		LastCooldownSeconds:                 60,
		MaxCooldownStrikes:                  1,
		BanMode:                             "off",
		BlockedUntilAt:                      &pastProbeEligibleAt,
		ProbeAvailableAt:                    &pastProbeEligibleAt,
		CircuitState:                        "open",
		LastSuccessResponseHeadersLatencyMS: &staleLatency,
		LastLiveFailureKind:                 &priorFailureKind,
		LastLiveFailureAt:                   &pastProbeEligibleAt,
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "probe success mutation"), nil)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if successState.ConsecutiveFailures != 0 || successState.CircuitState != "closed" {
		t.Fatalf("expected probe success to close and reset recovery state, got %+v", successState)
	}
	if successState.OpenUntilAt.Valid || successState.ProbeAvailableAt.Valid {
		t.Fatalf("expected probe success to clear open/probe timers, got %+v", successState)
	}
	if successState.LastFailureKind.Valid || successState.LastLiveFailureKind.Valid {
		t.Fatalf("expected probe success to clear failure markers, got %+v", successState)
	}
	if !successState.LastLiveSuccessAt.Valid || !successState.LastSuccessResponseHeadersLatencyMS.Valid || successState.LastSuccessResponseHeadersLatencyMS.Int32 < 1 {
		t.Fatalf("expected probe success to persist latency and success timestamp, got %+v", successState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, route.connectionIDs[0])
	assertLoadbalanceEventTypeSequence(t, events, "recovered", "unbanned")
	if !events[0].FailureKind.Valid || events[0].FailureKind.String != "transient_http" {
		t.Fatalf("expected recovered transient_http success event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != route.targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != route.endpointIDs[0] {
		t.Fatalf("expected recovery event model/endpoint snapshot %q/%d, got %+v", route.targetModelID, route.endpointIDs[0], events[0])
	}
	if !events[1].FailureKind.Valid || events[1].FailureKind.String != "transient_http" || events[1].ConsecutiveFailures != 1 || events[1].CooldownSeconds != 60 {
		t.Fatalf("expected trailing unbanned transient_http event after recovery, got %+v", events[1])
	}
}

func TestRuntimeStatePersistsRecoveredStateAcrossRestart(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-restart-recovered"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-restart-recovered",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: upstream.baseURL("/loadbalance/restart/recovered"), priority: 0}},
	})
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:                           activeProfileID,
		ConnectionID:                        route.connectionIDs[0],
		ConsecutiveFailures:                 1,
		LastFailureKind:                     &priorFailureKind,
		LastCooldownSeconds:                 60,
		MaxCooldownStrikes:                  1,
		BanMode:                             "off",
		BlockedUntilAt:                      &pastProbeEligibleAt,
		ProbeAvailableAt:                    &pastProbeEligibleAt,
		CircuitState:                        "open",
		LastSuccessResponseHeadersLatencyMS: &staleLatency,
		LastLiveFailureKind:                 &priorFailureKind,
		LastLiveFailureAt:                   &pastProbeEligibleAt,
	})

	initialResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "restart recovered persistence initial mutation"), nil)
	assertStatus(t, initialResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected initial recovery request to hit the upstream once, got %d requests", got)
	}

	recoveredStateBeforeRestart := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if recoveredStateBeforeRestart.ConsecutiveFailures != 0 || recoveredStateBeforeRestart.CircuitState != "closed" {
		t.Fatalf("expected successful recovery to persist a closed state before restart, got %+v", recoveredStateBeforeRestart)
	}
	if recoveredStateBeforeRestart.OpenUntilAt.Valid || recoveredStateBeforeRestart.ProbeAvailableAt.Valid {
		t.Fatalf("expected successful recovery to clear open/probe timers before restart, got %+v", recoveredStateBeforeRestart)
	}
	if !recoveredStateBeforeRestart.LastLiveSuccessAt.Valid || !recoveredStateBeforeRestart.LastSuccessResponseHeadersLatencyMS.Valid {
		t.Fatalf("expected successful recovery to persist latency and success timestamp before restart, got %+v", recoveredStateBeforeRestart)
	}

	restartedHarness := restartRuntimeHarness(t, harness.databaseName)
	if runtimeStateExists(t, restartedHarness, activeProfileID, route.connectionIDs[0]) {
		t.Fatalf("expected restart to clear recovered runtime state for connection %d", route.connectionIDs[0])
	}

	restartedResponse := restartedHarness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "restart recovered persistence reload"), nil)
	assertStatus(t, restartedResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected restarted runtime to route through the primary again after ephemeral-state reset, got %d total requests", got)
	}
}
