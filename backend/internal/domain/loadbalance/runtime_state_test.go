package loadbalance

import (
	"reflect"
	"testing"
	"time"
)

func TestRuntimeConnectionStateIsEligible(t *testing.T) {
	nowAt := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)
	futureBan := nowAt.Add(15 * time.Minute)
	futureOpen := nowAt.Add(10 * time.Minute)
	futureProbe := nowAt.Add(5 * time.Minute)
	pastOpen := nowAt.Add(-1 * time.Minute)

	tests := []struct {
		name  string
		state RuntimeConnectionState
		want  bool
	}{
		{name: "manual ban excluded", state: RuntimeConnectionState{BanMode: "manual"}, want: false},
		{name: "temporary ban excluded", state: RuntimeConnectionState{BanMode: "temporary", BannedUntilAt: &futureBan}, want: false},
		{name: "open interval excluded", state: RuntimeConnectionState{BanMode: "off", OpenUntilAt: &futureOpen}, want: false},
		{name: "future probe on open circuit excluded", state: RuntimeConnectionState{BanMode: "off", CircuitState: "open", OpenUntilAt: &pastOpen, ProbeAvailableAt: &futureProbe}, want: false},
		{name: "past timers eligible", state: RuntimeConnectionState{BanMode: "off", CircuitState: "closed", OpenUntilAt: &pastOpen}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.state.IsEligible(nowAt); got != test.want {
				t.Fatalf("expected eligible=%t, got %t", test.want, got)
			}
		})
	}
}

func TestFilterEligibleConnectionIDsPreservesCandidateOrder(t *testing.T) {
	nowAt := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)
	blockedUntilAt := nowAt.Add(10 * time.Minute)
	candidates := []ConnectionOrderCandidate{{ID: 4, Priority: 0}, {ID: 2, Priority: 1}, {ID: 7, Priority: 2}}
	states := map[int]RuntimeConnectionState{
		4: {ConnectionID: 4, BanMode: "off", OpenUntilAt: &blockedUntilAt},
	}

	got := FilterEligibleConnectionIDs(candidates, states, nowAt)
	want := []int{2, 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected eligible ids %v, got %v", want, got)
	}
}
