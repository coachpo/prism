package loadbalance

import (
	"testing"
	"time"
)

func TestFilterEligibleFallsBackWhenEveryCandidateIsCoolingDown(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	soon := now.Add(45 * time.Second)
	candidates := []ConnectionOrderCandidate{{ID: 1, Priority: 1}, {ID: 2, Priority: 2}}
	states := map[int]RuntimeConnectionState{
		1: {ConnectionID: 1, BanMode: "off", NextRetryAt: &soon},
		2: {ConnectionID: 2, BanMode: "off", NextRetryAt: &soon},
	}

	eligible := FilterEligibleConnectionIDs(candidates, states, now)
	if len(eligible) != 2 {
		t.Fatalf("every candidate cooling down must still yield candidates, got %v", eligible)
	}
	if eligible[0] != 1 || eligible[1] != 2 {
		t.Fatalf("fallback must preserve candidate order, got %v", eligible)
	}
}

func TestFilterEligiblePrefersAvailableOverCoolingDown(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	soon := now.Add(45 * time.Second)
	candidates := []ConnectionOrderCandidate{{ID: 1, Priority: 1}, {ID: 2, Priority: 2}}
	states := map[int]RuntimeConnectionState{
		1: {ConnectionID: 1, BanMode: "off", NextRetryAt: &soon},
	}

	eligible := FilterEligibleConnectionIDs(candidates, states, now)
	if len(eligible) != 1 || eligible[0] != 2 {
		t.Fatalf("a healthy candidate must win over one in backoff, got %v", eligible)
	}
}

func TestFilterEligibleKeepsHonouringExplicitBans(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	soon := now.Add(45 * time.Second)
	later := now.Add(10 * time.Minute)
	candidates := []ConnectionOrderCandidate{{ID: 1, Priority: 1}, {ID: 2, Priority: 2}, {ID: 3, Priority: 3}}
	states := map[int]RuntimeConnectionState{
		// An operator ban and a policy ban are decisions, not backoff hints, and
		// must survive the last-resort fallback.
		1: {ConnectionID: 1, BanMode: "until_reset"},
		2: {ConnectionID: 2, BanMode: "temporary", BannedUntilAt: &later},
		3: {ConnectionID: 3, BanMode: "off", NextRetryAt: &soon},
	}

	eligible := FilterEligibleConnectionIDs(candidates, states, now)
	if len(eligible) != 1 || eligible[0] != 3 {
		t.Fatalf("banned connections must stay excluded from the fallback, got %v", eligible)
	}
}

func TestFilterEligibleReturnsNothingWhenEveryCandidateIsBanned(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	later := now.Add(10 * time.Minute)
	candidates := []ConnectionOrderCandidate{{ID: 1, Priority: 1}}
	states := map[int]RuntimeConnectionState{
		1: {ConnectionID: 1, BanMode: "temporary", BannedUntilAt: &later},
	}

	if eligible := FilterEligibleConnectionIDs(candidates, states, now); len(eligible) != 0 {
		t.Fatalf("an all-banned model must still fail closed, got %v", eligible)
	}
}

func TestDefaultFailoverStatusCodesCoverExpiredCredentials(t *testing.T) {
	// 401 is the shape an expired or revoked upstream key takes; without it a
	// model with healthy backups returned the first target's 401 unchanged.
	for _, code := range []int{401, 408} {
		found := false
		for _, candidate := range defaultRuntimeFailoverStatusCodes {
			if candidate == code {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("default failover set must contain %d, got %v", code, defaultRuntimeFailoverStatusCodes)
		}
	}
}
