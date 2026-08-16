package runtime

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// Truth table for the scheduleClosedOnly / scheduleUnresolvableOnly whitelists:
// every combination of the three facts that decides the outcome.
func TestScheduleWhitelistsTruthTable(t *testing.T) {
	cases := []struct {
		name             string
		excluded         []int
		unresolvable     []int
		evaluated        []int
		otherExclusion   bool
		wantClosed       bool
		wantUnresolvable bool
	}{
		{"pure closed", []int{7}, nil, []int{7}, false, true, false},
		{"pure unresolvable", nil, []int{7}, []int{7}, false, false, true},
		{"mixed closed and unresolvable", []int{7}, []int{8}, []int{7, 8}, false, false, true},
		{"partial exclusion", []int{7}, nil, []int{7, 9}, false, false, false},
		{"banned peer alongside", []int{7}, nil, []int{7, 9}, true, false, false},
		{"other exclusion alongside unresolvable", nil, []int{7}, []int{7}, true, false, false},
		{"nothing evaluated", nil, nil, nil, false, false, false},
		{"excluded without evaluated", []int{7}, nil, nil, false, false, false},
		{"diamond dedup keeps pure closed", []int{7}, nil, []int{7, 7}, false, true, false},
		{"diamond dedup keeps pure unresolvable", nil, []int{7}, []int{7, 7}, false, false, true},
		{"zero value observation", nil, nil, nil, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := &runtimePlanningObservation{}
			for _, id := range tc.excluded {
				observation.recordScheduleExclusion(id, true)
			}
			for _, id := range tc.unresolvable {
				observation.recordScheduleUnresolvable(id)
			}
			for _, id := range tc.evaluated {
				observation.recordEvaluatedTerminalConnection(id)
			}
			if tc.otherExclusion {
				observation.recordOtherExclusion()
			}
			if got := observation.scheduleClosedOnly(); got != tc.wantClosed {
				t.Fatalf("scheduleClosedOnly = %v, want %v (observation %+v)", got, tc.wantClosed, observation)
			}
			if got := observation.scheduleUnresolvableOnly(); got != tc.wantUnresolvable {
				t.Fatalf("scheduleUnresolvableOnly = %v, want %v (observation %+v)", got, tc.wantUnresolvable, observation)
			}
		})
	}
}

func TestScheduleWhitelistNonAttributableExclusionCountsAsOther(t *testing.T) {
	observation := &runtimePlanningObservation{}
	observation.recordEvaluatedTerminalConnection(7)
	observation.recordScheduleExclusion(7, false)
	if observation.scheduleClosedOnly() {
		t.Fatalf("a non-attributable exclusion must mark the cause as mixed")
	}
	if !observation.OtherExclusionSeen {
		t.Fatalf("expected OtherExclusionSeen to be set by a non-attributable exclusion")
	}
}

func TestMergeSwallowedChildObservationEquivalence(t *testing.T) {
	// Merging a child's sets with union plus OtherExclusionSeen OR-float is
	// equivalent to running the whitelist on the child first: a child that is
	// not a pure schedule failure has OtherExclusionSeen set, which disables
	// the parent's whitelist; a pure child floats equal numerator and
	// denominator so the parent's condition 3 stays true.
	cases := []struct {
		name       string
		child      *runtimePlanningObservation
		wantParent bool
	}{
		{
			name: "pure closed child keeps parent pure",
			child: func() *runtimePlanningObservation {
				o := &runtimePlanningObservation{}
				o.recordEvaluatedTerminalConnection(7)
				o.recordScheduleExclusion(7, true)
				return o
			}(),
			wantParent: true,
		},
		{
			name: "mixed child disables parent whitelist",
			child: func() *runtimePlanningObservation {
				o := &runtimePlanningObservation{}
				o.recordEvaluatedTerminalConnection(7)
				o.recordScheduleExclusion(7, true)
				o.recordOtherExclusion()
				return o
			}(),
			wantParent: false,
		},
		{
			name: "child with banned sibling disables parent whitelist",
			child: func() *runtimePlanningObservation {
				o := &runtimePlanningObservation{}
				o.recordEvaluatedTerminalConnection(7)
				o.recordEvaluatedTerminalConnection(8)
				o.recordScheduleExclusion(7, true)
				o.recordOtherExclusion()
				return o
			}(),
			wantParent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := &runtimePlanningObservation{}
			parent.mergeSwallowedChildObservation(tc.child)
			if got := parent.scheduleClosedOnly(); got != tc.wantParent {
				t.Fatalf("parent scheduleClosedOnly = %v, want %v (parent %+v)", got, tc.wantParent, parent)
			}
		})
	}
}

func TestScheduleObservationNilSafety(t *testing.T) {
	var observation *runtimePlanningObservation
	observation.recordCompatibleStaticRoute()
	observation.recordEvaluatedTerminalConnection(1)
	observation.recordScheduleExclusion(1, true)
	observation.recordScheduleUnresolvable(1)
	observation.recordOtherExclusion()
	observation.absorbChildStaticRoute(&runtimePlanningObservation{})
	observation.mergeSwallowedChildObservation(&runtimePlanningObservation{})
	if observation.scheduleClosedOnly() || observation.scheduleUnresolvableOnly() {
		t.Fatalf("nil observation must report false for both whitelists")
	}
	if observation != nil {
		t.Fatalf("methods must not allocate on nil receivers")
	}
}

func TestScheduleClosedWireShape(t *testing.T) {
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"})
	withRoutingSchedule(snapshot, 1001, requestPlanMondayOnlySchedule())
	observation := &runtimePlanningObservation{}
	observation.recordEvaluatedTerminalConnection(1001)
	observation.recordScheduleExclusion(1001, true)

	// Unattributed: with another open connection evaluated, the whitelist
	// fails and scheduleRejectionError returns nil.
	observation.recordEvaluatedTerminalConnection(1002)
	if err := scheduleRejectionError(nil, "router-openai", observation, requestPlanFixedNow); err != nil {
		t.Fatalf("expected nil for a partial exclusion, got %v", err)
	}

	// Attributed: only the closed connection evaluated.
	attributed := &runtimePlanningObservation{}
	attributed.recordEvaluatedTerminalConnection(1001)
	attributed.recordScheduleExclusion(1001, true)
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		t.Fatalf("compile routing plan: %v", err)
	}
	err = scheduleRejectionError(routingPlan, "router-openai", attributed, requestPlanFixedNow)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected closed domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusServiceUnavailable || domainErr.ErrorCode != terminalTargetScheduleClosedErrorCode {
		t.Fatalf("expected 503 closed code, got %d %q", domainErr.StatusCode, domainErr.ErrorCode)
	}
	if !strings.Contains(domainErr.Detail, "outside their configured routing window") {
		t.Fatalf("expected window detail, got %q", domainErr.Detail)
	}
	if strings.Contains(domainErr.Detail, "All") {
		t.Fatalf("detail must never claim All, got %q", domainErr.Detail)
	}
	if domainErr.Fields["schedule_excluded_connection_count"] != 1 {
		t.Fatalf("expected real exclusion count, got %+v", domainErr.Fields)
	}
	if _, ok := domainErr.Fields["schedule_earliest_next_open_at"]; !ok {
		t.Fatalf("expected earliest next open key when known, got %+v", domainErr.Fields)
	}
	if _, ok := domainErr.Fields["schedule_earliest_next_open_at_known"]; !ok {
		t.Fatalf("expected next open known flag, got %+v", domainErr.Fields)
	}
	if domainErr.Fields["schedule_earliest_next_open_at_known"] != true {
		t.Fatalf("expected next open known true, got %+v", domainErr.Fields)
	}
}

func TestScheduleUnresolvableWireShape(t *testing.T) {
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"})
	withRoutingSchedule(snapshot, 1001, terminaltarget.CompileRoutingSchedule("Not/AZone", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 60}}))
	observation := &runtimePlanningObservation{}
	observation.recordEvaluatedTerminalConnection(1001)
	observation.recordScheduleUnresolvable(1001)
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		t.Fatalf("compile routing plan: %v", err)
	}
	err = scheduleRejectionError(routingPlan, "router-openai", observation, requestPlanFixedNow)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected unresolvable domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusServiceUnavailable || domainErr.ErrorCode != terminalTargetScheduleUnresolvableErrorCode {
		t.Fatalf("expected 503 unresolvable code, got %d %q", domainErr.StatusCode, domainErr.ErrorCode)
	}
	if _, ok := domainErr.Fields["schedule_earliest_next_open_at"]; ok {
		t.Fatalf("unresolvable code must not promise a reopen, got %+v", domainErr.Fields)
	}
}

func TestScheduleUnresolvableBeatsClosed(t *testing.T) {
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-openai"},
	)
	withRoutingSchedule(snapshot, 1001, terminaltarget.CompileRoutingSchedule("Not/AZone", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 60}}))
	withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
	observation := &runtimePlanningObservation{}
	observation.recordEvaluatedTerminalConnection(1001)
	observation.recordEvaluatedTerminalConnection(1002)
	observation.recordScheduleUnresolvable(1001)
	observation.recordScheduleExclusion(1002, true)
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		t.Fatalf("compile routing plan: %v", err)
	}
	err = scheduleRejectionError(routingPlan, "router-openai", observation, requestPlanFixedNow)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.ErrorCode != terminalTargetScheduleUnresolvableErrorCode {
		t.Fatalf("expected unresolvable code to win over closed, got %q", domainErr.ErrorCode)
	}
}

func TestAnnotateSchedulePartialExclusionCopiesAndTruncates(t *testing.T) {
	original := &domainError{StatusCode: http.StatusServiceUnavailable, Detail: strings.Repeat("x", 4000)}
	observation := &runtimePlanningObservation{}
	observation.recordEvaluatedTerminalConnection(7)
	observation.recordEvaluatedTerminalConnection(8)
	observation.recordScheduleExclusion(7, true)
	observation.recordScheduleUnresolvable(8)

	annotated := annotateSchedulePartialExclusion(original, observation)
	if original.Detail != strings.Repeat("x", 4000) {
		t.Fatalf("annotate must not mutate the input error")
	}
	if annotated == original {
		t.Fatalf("expected a copied error, got the same pointer")
	}
	if !strings.Contains(annotated.Detail, "1 of 2 terminal targets were outside their routing window.") ||
		!strings.Contains(annotated.Detail, "1 of 2 terminal targets have an unresolvable routing timezone.") {
		t.Fatalf("expected both mixed-cause sentences, got %q", annotated.Detail)
	}

	if got := annotateSchedulePartialExclusion(original, &runtimePlanningObservation{}); got != original {
		t.Fatalf("empty observation must return the input untouched")
	}
	if got := annotateSchedulePartialExclusion(original, nil); got != original {
		t.Fatalf("nil observation must return the input untouched")
	}
	if got := annotateSchedulePartialExclusion(nil, observation); got != nil {
		t.Fatalf("nil error must stay nil")
	}
}

func TestScheduleCodesAreSafediagValid(t *testing.T) {
	// The two codes must persist verbatim in request_logs; a code failing
	// safediag.ValidErrorCode would degrade to prism_routing_failure.
	for _, code := range []string{terminalTargetScheduleClosedErrorCode, terminalTargetScheduleUnresolvableErrorCode} {
		if len(code) > 120 {
			t.Fatalf("code %q exceeds request_logs.error_code width", code)
		}
		for _, r := range code {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == ':' || r == '-') {
				t.Fatalf("code %q contains illegal character %q", code, r)
			}
		}
	}
}
