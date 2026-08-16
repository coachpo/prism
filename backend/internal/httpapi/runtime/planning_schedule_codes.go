package runtime

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

// Stable family-neutral planning rejection codes for routing-window
// exclusions. Both codes satisfy safediag.ValidErrorCode, so the observability
// layer persists them verbatim instead of degrading to prism_routing_failure;
// request_logs.error_code has a syntax regex but no value enum, so no schema
// change is needed.
const (
	terminalTargetScheduleClosedErrorCode       = "terminal_target_schedule_closed"
	terminalTargetScheduleUnresolvableErrorCode = "terminal_target_schedule_unresolvable"
)

// scheduleExcludedConnectionIDsWireLimit caps the per-code connection ID list
// on the wire; the counts always carry the real totals.
const scheduleExcludedConnectionIDsWireLimit = 20

// The runtimePlanningObservation methods live in this file (the struct is
// declared in planning_snapshot.go). Every method is nil-receiver safe with
// lazy map allocation: the three dead wrappers and future unit tests may
// construct bare contexts, and a nil-map write would panic into a 500 that
// the runtime.go:674-677 translation gate would then reject, leaving zero
// request_logs evidence.

func (o *runtimePlanningObservation) recordCompatibleStaticRoute() {
	if o == nil {
		return
	}
	o.CompatibleStaticRouteSeen = true
}

// recordEvaluatedTerminalConnection records the denominator: a connection
// that was actually evaluated for this resolution, deduplicated by
// connection ID so diamond authorization graphs cannot inflate the count.
func (o *runtimePlanningObservation) recordEvaluatedTerminalConnection(connectionID int) {
	if o == nil {
		return
	}
	if o.EvaluatedTerminalConnectionIDs == nil {
		o.EvaluatedTerminalConnectionIDs = map[int]struct{}{}
	}
	o.EvaluatedTerminalConnectionIDs[connectionID] = struct{}{}
}

// recordScheduleExclusion records the numerator for the closed whitelist.
// A connection excluded from routing that is NOT schedule-attributable (an
// OpenAI capability mismatch on top of the window) marks the cause as mixed:
// the stable schedule code must not fire for it.
func (o *runtimePlanningObservation) recordScheduleExclusion(connectionID int, attributable bool) {
	if o == nil {
		return
	}
	if !attributable {
		o.recordOtherExclusion()
		return
	}
	if o.ScheduleExcludedConnectionIDs == nil {
		o.ScheduleExcludedConnectionIDs = map[int]struct{}{}
	}
	o.ScheduleExcludedConnectionIDs[connectionID] = struct{}{}
}

func (o *runtimePlanningObservation) recordScheduleUnresolvable(connectionID int) {
	if o == nil {
		return
	}
	if o.ScheduleUnresolvableConnectionIDs == nil {
		o.ScheduleUnresolvableConnectionIDs = map[int]struct{}{}
	}
	o.ScheduleUnresolvableConnectionIDs[connectionID] = struct{}{}
}

// recordOtherExclusion marks any exclusion that is not a routing-window
// cause: depth/cycle/missing strategy, capability gates, missing targets,
// ban exhaustion, structural misses. It is the anti-whitelist bit.
func (o *runtimePlanningObservation) recordOtherExclusion() {
	if o == nil {
		return
	}
	o.OtherExclusionSeen = true
}

// scheduleClosedOnly is the whitelist for terminal_target_schedule_closed:
// at least one schedule exclusion, no other exclusion reason, no unresolvable
// timezone, and the exclusion set equals the evaluated set (both deduplicated
// by connection ID). All three conditions are required; a mixed cause must
// fall back to the ordinary 503 plus detail hints.
func (o *runtimePlanningObservation) scheduleClosedOnly() bool {
	if o == nil {
		return false
	}
	return len(o.ScheduleExcludedConnectionIDs) > 0 &&
		!o.OtherExclusionSeen && len(o.ScheduleUnresolvableConnectionIDs) == 0 &&
		len(o.ScheduleExcludedConnectionIDs) == len(o.EvaluatedTerminalConnectionIDs)
}

// scheduleUnresolvableOnly is the same whitelist shape for
// terminal_target_schedule_unresolvable. The two sets are mutually exclusive
// by construction (the gate's first branch returns before the second), and
// both sides are idempotent under diamond-graph re-evaluation.
func (o *runtimePlanningObservation) scheduleUnresolvableOnly() bool {
	if o == nil {
		return false
	}
	return len(o.ScheduleUnresolvableConnectionIDs) > 0 &&
		!o.OtherExclusionSeen &&
		len(o.ScheduleUnresolvableConnectionIDs)+len(o.ScheduleExcludedConnectionIDs) == len(o.EvaluatedTerminalConnectionIDs)
}

// absorbChildStaticRoute floats the child's CompatibleStaticRouteSeen up.
// It is called unconditionally, before every error branch, so nested graphs
// keep the classification they had when the child wrote the shared
// observation directly.
func (o *runtimePlanningObservation) absorbChildStaticRoute(child *runtimePlanningObservation) {
	if o == nil || child == nil {
		return
	}
	o.CompatibleStaticRouteSeen = o.CompatibleStaticRouteSeen || child.CompatibleStaticRouteSeen
}

// mergeSwallowedChildObservation unions a child observation whose noEligible
// failure was swallowed. Unions keep the parent's own recorded facts (a
// parent with direct peers must not lose its denominator), and OtherExclusionSeen
// must float up with OR: a subtree with a mixed cause would otherwise let the
// parent whitelist a pure schedule attribution and promise a next_open_at
// that is still fully dark. This union-plus-OR-float is mathematically
// equivalent to running the whitelist on the subtree first, which is why the
// whitelist itself never needs to run on children.
func (o *runtimePlanningObservation) mergeSwallowedChildObservation(child *runtimePlanningObservation) {
	if o == nil || child == nil {
		return
	}
	o.unionScheduleSet(&o.EvaluatedTerminalConnectionIDs, child.EvaluatedTerminalConnectionIDs)
	o.unionScheduleSet(&o.ScheduleExcludedConnectionIDs, child.ScheduleExcludedConnectionIDs)
	o.unionScheduleSet(&o.ScheduleUnresolvableConnectionIDs, child.ScheduleUnresolvableConnectionIDs)
	o.OtherExclusionSeen = o.OtherExclusionSeen || child.OtherExclusionSeen
}

func (o *runtimePlanningObservation) unionScheduleSet(destination *map[int]struct{}, source map[int]struct{}) {
	for connectionID := range source {
		if *destination == nil {
			*destination = map[int]struct{}{}
		}
		(*destination)[connectionID] = struct{}{}
	}
}

// terminalTargetScheduleAttributable reports whether a window exclusion of
// this connection can be attributed to the schedule at all. The model-side
// dimensions must be the requested-operation dimensions (ctx.RequestedOpenAI*),
// not runtimeModelCapabilityDimensions(sourceModel) (that is the snapshot-side
// view used elsewhere); the family check is on the terminal's source model
// (attempt.TargetModel.APIFamily), never connection.APIFamily.
func terminalTargetScheduleAttributable(sourceModel runtimeModelRecord, connection runtimeConnection, ctx runtimeAccessResolutionContext) bool {
	if !runtimeOperationIsOpenAICapabilityGated(ctx.RequestOperation) {
		return true
	}
	if !providerauth.IsOpenAI(sourceModel.APIFamily) {
		return false
	}
	_, supported := resolveTranslationMode(ctx.RequestOperation,
		runtimeOpenAICapabilityDimensions{TextMode: ctx.RequestedOpenAIAcceptedFormat, ImageOperations: ctx.RequestedOpenAIImageOperations},
		runtimeConnectionCapabilityDimensions(connection))
	return supported
}

// scheduleRejectionError classifies a planning failure as a pure
// routing-window rejection when either whitelist holds. The unresolvable
// code wins over the closed code: a broken timezone is a configuration
// defect, while being outside a window is a normal strategy outcome.
func scheduleRejectionError(routingPlan *runtimeRoutingPlan, requestedModelID string, observation *runtimePlanningObservation, referenceNow time.Time) *domainError {
	if observation == nil {
		return nil
	}
	switch {
	case observation.scheduleUnresolvableOnly():
		return terminalTargetScheduleUnresolvableDomainError(routingPlan, requestedModelID, observation, referenceNow)
	case observation.scheduleClosedOnly():
		return terminalTargetScheduleClosedDomainError(routingPlan, requestedModelID, observation, referenceNow)
	}
	return nil
}

// terminalTargetScheduleClosedDomainError builds the 503 envelope for a pure
// window rejection. The detail deliberately does not say "All": under the
// single strategy only the first row is evaluated, so "All" would be
// falsifiable. schedule_earliest_next_open_at is only present when known —
// a typed nil pointer would marshal as JSON null and break the "key absent
// when _known is false" wire contract.
func terminalTargetScheduleClosedDomainError(routingPlan *runtimeRoutingPlan, requestedModelID string, observation *runtimePlanningObservation, referenceNow time.Time) *domainError {
	excludedIDs := sortedScheduleConnectionIDs(observation.ScheduleExcludedConnectionIDs)
	detail := fmt.Sprintf("%d evaluated terminal target(s) for model '%s' are outside their configured routing window.", len(excludedIDs), requestedModelID)
	fields := map[string]any{
		"schedule_excluded_connection_ids_truncated": len(excludedIDs) > scheduleExcludedConnectionIDsWireLimit,
		"schedule_excluded_connection_count":         len(observation.ScheduleExcludedConnectionIDs),
		"schedule_reference_now":                     referenceNow.UTC().Format(time.RFC3339),
	}
	if len(excludedIDs) > scheduleExcludedConnectionIDsWireLimit {
		excludedIDs = excludedIDs[:scheduleExcludedConnectionIDsWireLimit]
	}
	if len(excludedIDs) > 0 {
		fields["schedule_excluded_connection_ids"] = excludedIDs
	}
	if nextOpenAt, known := earliestScheduleReopen(routingPlan, observation, referenceNow); known {
		fields["schedule_earliest_next_open_at"] = nextOpenAt.UTC().Format(time.RFC3339)
		fields["schedule_earliest_next_open_at_known"] = true
		detail += " The earliest window reopens at " + nextOpenAt.UTC().Format(time.RFC3339) + "."
	} else {
		fields["schedule_earliest_next_open_at_known"] = false
	}
	return &domainError{StatusCode: http.StatusServiceUnavailable, ErrorCode: terminalTargetScheduleClosedErrorCode, Detail: detail, Fields: fields}
}

// terminalTargetScheduleUnresolvableDomainError builds the 503 envelope for
// a pure unresolvable-timezone rejection. The excluded set has no computed
// reopen (NextOpenAt is unknown for Unresolved schedules), so the wire shape
// carries the ID list and counts only.
func terminalTargetScheduleUnresolvableDomainError(routingPlan *runtimeRoutingPlan, requestedModelID string, observation *runtimePlanningObservation, referenceNow time.Time) *domainError {
	unresolvableIDs := sortedScheduleConnectionIDs(observation.ScheduleUnresolvableConnectionIDs)
	detail := fmt.Sprintf("%d evaluated terminal target(s) for model '%s' have an unresolvable routing timezone.", len(unresolvableIDs), requestedModelID)
	fields := map[string]any{
		"schedule_unresolvable_connection_ids_truncated": len(unresolvableIDs) > scheduleExcludedConnectionIDsWireLimit,
		"schedule_unresolvable_connection_count":         len(observation.ScheduleUnresolvableConnectionIDs),
		"schedule_reference_now":                         referenceNow.UTC().Format(time.RFC3339),
	}
	if len(unresolvableIDs) > scheduleExcludedConnectionIDsWireLimit {
		unresolvableIDs = unresolvableIDs[:scheduleExcludedConnectionIDsWireLimit]
	}
	if len(unresolvableIDs) > 0 {
		fields["schedule_unresolvable_connection_ids"] = unresolvableIDs
	}
	return &domainError{StatusCode: http.StatusServiceUnavailable, ErrorCode: terminalTargetScheduleUnresolvableErrorCode, Detail: detail, Fields: fields}
}

// earliestScheduleReopen is the only NextOpenAt call site. It scans only the
// excluded connections (bounded by the wire limit in practice) and returns
// the earliest known reopen instant.
func earliestScheduleReopen(routingPlan *runtimeRoutingPlan, observation *runtimePlanningObservation, referenceNow time.Time) (time.Time, bool) {
	earliest := time.Time{}
	found := false
	for connectionID := range observation.ScheduleExcludedConnectionIDs {
		connection, ok := routingPlan.TerminalTargetsByID[connectionID]
		if !ok {
			continue
		}
		nextOpenAt, known := connection.RoutingSchedule.NextOpenAt(referenceNow)
		if !known {
			continue
		}
		if !found || nextOpenAt.Before(earliest) {
			earliest = nextOpenAt
			found = true
		}
	}
	return earliest, found
}

// annotateSchedulePartialExclusion appends at most two detail sentences when
// a planning failure mixes routing-window causes with other causes. The
// stable codes must not fire (the whitelist failed), so request_logs Detail
// is the only channel: requestLogInsert carries no fields, and the two
// sentences keep the mixed shape searchable. It copies before modifying — the
// caller's error must never be mutated.
func annotateSchedulePartialExclusion(err *domainError, observation *runtimePlanningObservation) *domainError {
	if err == nil || observation == nil {
		return err
	}
	excluded := len(observation.ScheduleExcludedConnectionIDs)
	unresolvable := len(observation.ScheduleUnresolvableConnectionIDs)
	evaluated := len(observation.EvaluatedTerminalConnectionIDs)
	if excluded == 0 && unresolvable == 0 {
		return err
	}
	annotated := *err
	if excluded > 0 {
		annotated.Detail += fmt.Sprintf(" %d of %d terminal targets were outside their routing window.", excluded, evaluated)
	}
	if unresolvable > 0 {
		annotated.Detail += fmt.Sprintf(" %d of %d terminal targets have an unresolvable routing timezone.", unresolvable, evaluated)
	}
	return &annotated
}

func sortedScheduleConnectionIDs(connectionIDs map[int]struct{}) []int {
	sorted := make([]int, 0, len(connectionIDs))
	for connectionID := range connectionIDs {
		sorted = append(sorted, connectionID)
	}
	sort.Ints(sorted)
	return sorted
}
