package loadbalance

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestObservationCloneDoesNotRegisterOrMutateLiveState(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	clone, err := store.CloneForObservation(1, 20)
	if err != nil {
		t.Fatal(err)
	}
	clone.ClaimRoundRobinTargetCursor(1, 1, 1, "a", 2)
	clone.SnapshotConnectionStates(1, []RuntimeConnectionRef{{ConnectionID: 1, ModelConfigID: 1}})
	if len(store.profiles) != 0 {
		t.Fatal("observation registered live profile")
	}
	now := time.Now().UTC()
	store.SeedConnectionState(1, 1, 1, RuntimeConnectionState{ConnectionID: 1, BanMode: "until_reset", CumulativeRetryAttempts: 5}, now, now)
	store.ClaimRoundRobinTargetCursor(1, 1, 1, "a", 2)
	clone, err = store.CloneForObservation(1, 20)
	if err != nil {
		t.Fatal(err)
	}
	clone.ResetConnectionCooldown(1, 1, now)
	if next := clone.ClaimRoundRobinTargetCursor(1, 1, 1, "a", 2); next != 1 {
		t.Fatal("cursor was not sampled")
	}
	if next := store.ClaimRoundRobinTargetCursor(1, 1, 1, "a", 2); next != 1 {
		t.Fatal("private claim changed live cursor")
	}
	profile, _ := store.lookupProfile(1)
	if profile.connections[1].state.BanMode != "until_reset" {
		t.Fatal("private cooldown reset changed shared state")
	}
	if _, err := store.CloneForObservation(1, 1); err == nil {
		t.Fatal("expected bounded failure")
	}
}

func TestObservationCloneConcurrentMutationIsIndependent(t *testing.T) {
	store := NewLocalRuntimeStateStore()
	now := time.Now().UTC()
	store.SeedConnectionState(1, 1, 1, RuntimeConnectionState{ConnectionID: 1, BanMode: "off"}, now, now)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			store.ClaimRoundRobinTargetCursor(1, 1, 1, "a", 2)
			store.SeedConnectionState(1, 1, 1, RuntimeConnectionState{ConnectionID: 1, WindowRequestCount: i}, now, now)
		}
	}()
	for i := 0; i < 100; i++ {
		cloned, err := store.CloneForObservation(1, 20)
		if err != nil {
			t.Fatal(err)
		}
		refs := []RuntimeConnectionRef{{ConnectionID: 1, ModelConfigID: 1}}
		a := cloned.SnapshotConnectionStates(1, refs)
		b := cloned.SnapshotConnectionStates(1, refs)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("sample moved with source")
		}
	}
	wg.Wait()
}
