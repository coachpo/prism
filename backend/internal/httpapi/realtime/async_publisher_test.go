package realtime

import (
	"context"
	"sync"
	"testing"
	"time"
)

type asyncPublishCall struct {
	ProfileID    int
	RequestLogID int
}

type fakeAsyncDashboardTarget struct {
	mu           sync.Mutex
	latest       map[int]int
	subscribers  map[int]bool
	calls        []asyncPublishCall
	publishCh    chan asyncPublishCall
	firstStarted chan struct{}
	releaseFirst chan struct{}
	blockFirst   bool
}

func TestAsyncDashboardPublisher_CoalescesProfileWhileWorkerIsInflight(t *testing.T) {
	target := newFakeAsyncDashboardTarget(true)
	publisher := NewAsyncDashboardPublisher(target, AsyncDashboardPublisherOptions{
		QueueCapacity:   1,
		WorkerCount:     1,
		PublishTimeout:  5 * time.Second,
		ShutdownTimeout: time.Second,
	})
	defer publisher.Close()

	accepted, err := publisher.PublishDashboardUpdate(context.Background(), 101, 7)
	if err != nil || !accepted {
		t.Fatalf("expected first async dashboard publish to queue successfully, got accepted=%v err=%v", accepted, err)
	}
	target.waitUntilFirstStarted(t, 2*time.Second)
	accepted, err = publisher.PublishDashboardUpdate(context.Background(), 202, 7)
	if err != nil || !accepted {
		t.Fatalf("expected second async dashboard publish to coalesce successfully, got accepted=%v err=%v", accepted, err)
	}
	snapshot := publisher.Snapshot()
	if snapshot.QueueDepth != 0 || snapshot.InflightProfiles != 1 || snapshot.TrackedProfiles != 1 {
		t.Fatalf("expected inflight coalesced snapshot to track one inflight profile without queued backlog, got %+v", snapshot)
	}
	if snapshot.AcceptedCount != 1 || snapshot.CoalescedCount != 1 || snapshot.DroppedCount != 0 {
		t.Fatalf("expected coalesced counters accepted=1 coalesced=1 dropped=0, got %+v", snapshot)
	}
	if snapshot.Drained || snapshot.BusySince.IsZero() {
		t.Fatalf("expected inflight coalesced snapshot to remain busy, got %+v", snapshot)
	}
	if !snapshot.LastDrainedAt.IsZero() || snapshot.LastDrainDuration != 0 {
		t.Fatalf("expected no drain metadata before inflight work completes, got %+v", snapshot)
	}
	target.releaseBlockedPublish()

	firstCall := target.waitForCall(t, 2*time.Second)
	secondCall := target.waitForCall(t, 2*time.Second)
	if firstCall.ProfileID != 7 || firstCall.RequestLogID != 101 {
		t.Fatalf("expected first publish call to use original request log, got %+v", firstCall)
	}
	if secondCall.ProfileID != 7 || secondCall.RequestLogID != 202 {
		t.Fatalf("expected second publish call to use coalesced latest request log, got %+v", secondCall)
	}
	finalSnapshot := waitForAsyncDashboardDrain(t, publisher, 2*time.Second)
	if finalSnapshot.AcceptedCount != 1 || finalSnapshot.CoalescedCount != 1 || finalSnapshot.DroppedCount != 0 {
		t.Fatalf("expected final coalesced counters accepted=1 coalesced=1 dropped=0, got %+v", finalSnapshot)
	}
	if finalSnapshot.QueueDepth != 0 || finalSnapshot.InflightProfiles != 0 || finalSnapshot.TrackedProfiles != 0 || !finalSnapshot.Drained {
		t.Fatalf("expected coalesced publisher to drain fully after release, got %+v", finalSnapshot)
	}
	if finalSnapshot.LastDrainedAt.IsZero() || finalSnapshot.LastDrainDuration <= 0 {
		t.Fatalf("expected coalesced publisher to record a positive drain interval, got %+v", finalSnapshot)
	}
}

func TestAsyncDashboardPublisher_DropsOnlyQueuedBestEffortProfileWhenCapacityIsExhausted(t *testing.T) {
	target := newFakeAsyncDashboardTarget(true)
	publisher := NewAsyncDashboardPublisher(target, AsyncDashboardPublisherOptions{
		QueueCapacity:   1,
		WorkerCount:     1,
		PublishTimeout:  5 * time.Second,
		ShutdownTimeout: time.Second,
	})
	defer publisher.Close()

	accepted, err := publisher.PublishDashboardUpdate(context.Background(), 101, 1)
	if err != nil || !accepted {
		t.Fatalf("expected first async dashboard publish to queue successfully, got accepted=%v err=%v", accepted, err)
	}
	target.waitUntilFirstStarted(t, 2*time.Second)
	accepted, err = publisher.PublishDashboardUpdate(context.Background(), 201, 2)
	if err != nil || !accepted {
		t.Fatalf("expected second profile to queue while one worker is inflight, got accepted=%v err=%v", accepted, err)
	}
	accepted, err = publisher.PublishDashboardUpdate(context.Background(), 301, 3)
	if err != nil {
		t.Fatalf("expected saturated async publisher to drop without returning error, got %v", err)
	}
	if accepted {
		t.Fatal("expected third profile to drop once async publish capacity was exhausted")
	}
	snapshot := publisher.Snapshot()
	if snapshot.QueueDepth != 1 || snapshot.InflightProfiles != 1 || snapshot.TrackedProfiles != 2 {
		t.Fatalf("expected saturated snapshot to report one queued and one inflight profile, got %+v", snapshot)
	}
	if snapshot.AcceptedCount != 2 || snapshot.CoalescedCount != 0 || snapshot.DroppedCount != 1 {
		t.Fatalf("expected saturated counters accepted=2 coalesced=0 dropped=1, got %+v", snapshot)
	}
	if snapshot.Drained || snapshot.BusySince.IsZero() {
		t.Fatalf("expected saturated snapshot to remain busy before release, got %+v", snapshot)
	}
	if !snapshot.LastDrainedAt.IsZero() || snapshot.LastDrainDuration != 0 {
		t.Fatalf("expected no drain metadata before pressure is released, got %+v", snapshot)
	}

	delivered, err := publisher.PublishPendingDashboardUpdate(context.Background(), 2)
	if err != nil {
		t.Fatalf("skip duplicate pending publish for queued profile: %v", err)
	}
	if delivered {
		t.Fatal("expected queued profile pending replay to skip immediate duplicate delivery")
	}

	delivered, err = publisher.PublishPendingDashboardUpdate(context.Background(), 3)
	if err != nil {
		t.Fatalf("replay dropped profile latest dashboard update: %v", err)
	}
	if !delivered {
		t.Fatal("expected dropped live publish to remain replayable from the latest durable request log")
	}
	if replayCall := target.waitForCall(t, 2*time.Second); replayCall.ProfileID != 3 || replayCall.RequestLogID != 301 {
		t.Fatalf("expected replay call for dropped profile to use latest request log, got %+v", replayCall)
	}
	target.releaseBlockedPublish()
	finalSnapshot := waitForAsyncDashboardDrain(t, publisher, 2*time.Second)
	if finalSnapshot.AcceptedCount != 2 || finalSnapshot.CoalescedCount != 0 || finalSnapshot.DroppedCount != 1 {
		t.Fatalf("expected final saturated counters accepted=2 coalesced=0 dropped=1, got %+v", finalSnapshot)
	}
	if finalSnapshot.QueueDepth != 0 || finalSnapshot.InflightProfiles != 0 || finalSnapshot.TrackedProfiles != 0 || !finalSnapshot.Drained {
		t.Fatalf("expected saturated publisher to drain fully after release, got %+v", finalSnapshot)
	}
	if finalSnapshot.LastDrainedAt.IsZero() || finalSnapshot.LastDrainDuration <= 0 {
		t.Fatalf("expected saturated publisher to record a positive drain interval, got %+v", finalSnapshot)
	}
}

func newFakeAsyncDashboardTarget(blockFirst bool) *fakeAsyncDashboardTarget {
	return &fakeAsyncDashboardTarget{
		latest:       map[int]int{},
		subscribers:  map[int]bool{},
		publishCh:    make(chan asyncPublishCall, 8),
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		blockFirst:   blockFirst,
	}
}

func (t *fakeAsyncDashboardTarget) PublishLatestDashboardUpdate(ctx context.Context, profileID int) (int, bool, error) {
	t.mu.Lock()
	requestLogID := t.latest[profileID]
	call := asyncPublishCall{ProfileID: profileID, RequestLogID: requestLogID}
	t.calls = append(t.calls, call)
	callIndex := len(t.calls)
	t.mu.Unlock()

	if t.blockFirst && callIndex == 1 {
		select {
		case <-t.firstStarted:
		default:
			close(t.firstStarted)
		}
		select {
		case <-t.releaseFirst:
		case <-ctx.Done():
			return requestLogID, false, ctx.Err()
		}
	}
	select {
	case t.publishCh <- call:
	default:
	}
	return requestLogID, true, nil
}

func (t *fakeAsyncDashboardTarget) RecordLatestDashboardRequestLog(profileID int, requestLogID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latest[profileID] = requestLogID
}

func (t *fakeAsyncDashboardTarget) HasDashboardSubscribers(profileID int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	allowed, ok := t.subscribers[profileID]
	if !ok {
		return true
	}
	return allowed
}

func (t *fakeAsyncDashboardTarget) waitUntilFirstStarted(testingT *testing.T, timeout time.Duration) {
	testingT.Helper()
	select {
	case <-t.firstStarted:
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for the first async publish call to start")
	}
}

func (t *fakeAsyncDashboardTarget) releaseBlockedPublish() {
	select {
	case <-t.releaseFirst:
	default:
		close(t.releaseFirst)
	}
}

func (t *fakeAsyncDashboardTarget) waitForCall(testingT *testing.T, timeout time.Duration) asyncPublishCall {
	testingT.Helper()
	select {
	case call := <-t.publishCh:
		return call
	case <-time.After(timeout):
		testingT.Fatal("timed out waiting for async publish call")
		return asyncPublishCall{}
	}
}

func waitForAsyncDashboardDrain(testingT *testing.T, publisher *AsyncDashboardPublisher, timeout time.Duration) AsyncDashboardPublisherSnapshot {
	testingT.Helper()
	deadline := time.Now().Add(timeout)
	lastSnapshot := publisher.Snapshot()
	for time.Now().Before(deadline) {
		lastSnapshot = publisher.Snapshot()
		if lastSnapshot.Drained {
			return lastSnapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	testingT.Fatalf("timed out waiting for async dashboard publisher drain, last snapshot %+v", lastSnapshot)
	return AsyncDashboardPublisherSnapshot{}
}
