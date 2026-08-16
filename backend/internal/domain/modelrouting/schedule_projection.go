package modelrouting

import (
	"sort"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// Routing-schedule projection for static routing diagnostics.
//
// Everything here is a pure function of stored configuration. It deliberately
// never answers "is this window open right now": the analyzer is declared pure,
// and route_witness_generations guarantees that one generation always yields
// one analysis result. Admitting a clock would break that invariant, and
// LoadLocation results additionally vary with each instance's tzdata, which is
// not a property of the configuration being analyzed.

// DiagnosticsRoutingWindow is one window as authored.
type DiagnosticsRoutingWindow struct {
	WeekdayMask int `json:"weekday_mask"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

// DiagnosticsRoutingSchedule is the configuration view of a Terminal Target's
// routing schedule. CoversFullWeek is a set property of the windows, not a
// clock reading, so it stays stable for a given generation.
type DiagnosticsRoutingSchedule struct {
	Timezone       string                     `json:"timezone"`
	Windows        []DiagnosticsRoutingWindow `json:"windows"`
	CoversFullWeek bool                       `json:"covers_full_week"`
}

// routingScheduleProjection renders a connection's stored schedule, or nil when
// the connection has no windows and is therefore unrestricted.
func routingScheduleProjection(connection DiagnosticsConnection) *DiagnosticsRoutingSchedule {
	if len(connection.RoutingWindows) == 0 {
		return nil
	}
	projection := &DiagnosticsRoutingSchedule{
		Windows:        make([]DiagnosticsRoutingWindow, 0, len(connection.RoutingWindows)),
		CoversFullWeek: terminaltarget.WindowsCoverFullWeek(connection.RoutingWindows),
	}
	if connection.RoutingScheduleTimezone != nil {
		projection.Timezone = *connection.RoutingScheduleTimezone
	}
	for _, window := range connection.RoutingWindows {
		projection.Windows = append(projection.Windows, DiagnosticsRoutingWindow{
			WeekdayMask: window.WeekdayMask,
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}
	return projection
}

// scheduleAvailabilityFinding describes a routing-schedule availability gap for
// one operation, or nil when the operation's terminal leaves impose no
// schedule-driven risk.
type scheduleAvailabilityFinding struct {
	operationName string
	reason        string
	timezones     []string
	connectionIDs []int
}

// terminalLeavesForOperation collects the deduplicated terminal connections an
// operation actually resolves to.
//
// It reads the already-computed result rather than re-running
// stageCandidateForOperation: the analyzer has already subtracted disabled rows
// and the rows a `single` strategy truncates, and running the strategy walk a
// second time here could disagree with the result the caller is holding.
func terminalLeavesForOperation(result DiagnosticsResult, operationName string) []int {
	seen := map[int]struct{}{}
	leaves := make([]int, 0, 4)
	for _, target := range result.Targets {
		if target.ConnectionID == nil {
			continue
		}
		for _, operationResult := range target.OperationResults {
			if operationResult.OperationName != operationName {
				continue
			}
			for _, connectionID := range operationResult.TerminalConnectionIDs {
				if _, ok := seen[connectionID]; ok {
					continue
				}
				seen[connectionID] = struct{}{}
				leaves = append(leaves, connectionID)
			}
		}
	}
	sort.Ints(leaves)
	return leaves
}

// evaluateScheduleAvailability reports whether an operation's terminal leaves
// can leave it with no route at some point in the week.
//
// The check is strategy-agnostic: it asks only whether every leaf the operation
// can reach is schedule-restricted, and whether their windows together cover
// the whole week. A single unrestricted leaf makes the operation permanently
// routable and is reported as no finding at all, rather than as a milder
// warning, because there is nothing for an operator to act on.
func evaluateScheduleAvailability(graph *DiagnosticsGraph, result DiagnosticsResult, operationName string) *scheduleAvailabilityFinding {
	leaves := terminalLeavesForOperation(result, operationName)
	if len(leaves) == 0 {
		return nil
	}
	union := make([]terminaltarget.Window, 0, len(leaves)*2)
	timezoneSet := map[string]struct{}{}
	timezones := make([]string, 0, 2)
	for _, connectionID := range leaves {
		connection, ok := graph.ConnectionsByID[connectionID]
		if !ok || len(connection.RoutingWindows) == 0 {
			// One unrestricted leaf keeps the operation routable at every
			// instant, so no schedule finding applies.
			return nil
		}
		timezone := ""
		if connection.RoutingScheduleTimezone != nil {
			timezone = *connection.RoutingScheduleTimezone
		}
		if _, seen := timezoneSet[timezone]; !seen {
			timezoneSet[timezone] = struct{}{}
			timezones = append(timezones, timezone)
		}
		union = append(union, connection.RoutingWindows...)
	}
	sort.Strings(timezones)
	if len(timezones) > 1 {
		// Windows in different zones cannot be unioned on a single weekly
		// bitmap without picking one zone to project into, and that choice
		// would silently change the answer across DST transitions. Report the
		// mix and let the operator decide instead of guessing.
		return &scheduleAvailabilityFinding{
			operationName: operationName,
			reason:        ScheduleAvailabilityReasonMixedTimezones,
			timezones:     timezones,
			connectionIDs: leaves,
		}
	}
	if terminaltarget.WindowsCoverFullWeek(union) {
		return nil
	}
	return &scheduleAvailabilityFinding{
		operationName: operationName,
		reason:        ScheduleAvailabilityReasonUnionHasGaps,
		timezones:     timezones,
		connectionIDs: leaves,
	}
}
