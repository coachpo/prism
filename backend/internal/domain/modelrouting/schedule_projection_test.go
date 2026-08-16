package modelrouting

import (
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func scheduleTestGraph(connections map[int]DiagnosticsConnection) *DiagnosticsGraph {
	return &DiagnosticsGraph{ConnectionsByID: connections}
}

func scheduleTestConnection(id int, timezone string, windows ...terminaltarget.Window) DiagnosticsConnection {
	connection := DiagnosticsConnection{ID: id, RoutingWindows: windows}
	if timezone != "" {
		connection.RoutingScheduleTimezone = &timezone
	}
	return connection
}

func scheduleTestResult(operation string, connectionIDs ...int) DiagnosticsResult {
	targets := make([]DiagnosticsTarget, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		connectionID := id
		targets = append(targets, DiagnosticsTarget{
			ConnectionID:     &connectionID,
			OperationResults: []DiagnosticsOperationResult{{OperationName: operation, TerminalConnectionIDs: []int{connectionID}}},
		})
	}
	return DiagnosticsResult{Targets: targets}
}

// The finding is what drives the danger warning, so its edges matter more than
// the warning wrapper: an unrestricted leaf must silence it entirely, and a
// union that covers the week must not be reported as a gap.
func TestEvaluateScheduleAvailability(t *testing.T) {
	const operation = "openai.chat_completions"
	weekdayDay := terminaltarget.Window{WeekdayMask: 31, StartMinute: 540, EndMinute: 1080}
	for _, testCase := range []struct {
		name        string
		connections map[int]DiagnosticsConnection
		leaves      []int
		wantReason  string
	}{
		{
			name:        "no leaves yields no finding",
			connections: map[int]DiagnosticsConnection{},
			leaves:      nil,
		},
		{
			name: "an unrestricted leaf keeps the operation permanently routable",
			connections: map[int]DiagnosticsConnection{
				1: scheduleTestConnection(1, "Asia/Shanghai", weekdayDay),
				2: scheduleTestConnection(2, ""),
			},
			leaves: []int{1, 2},
		},
		{
			name: "a gap in the union is a finding",
			connections: map[int]DiagnosticsConnection{
				1: scheduleTestConnection(1, "Asia/Shanghai", weekdayDay),
			},
			leaves:     []int{1},
			wantReason: ScheduleAvailabilityReasonUnionHasGaps,
		},
		{
			name: "windows covering the whole week yield no finding",
			connections: map[int]DiagnosticsConnection{
				1: scheduleTestConnection(1, "Asia/Shanghai", terminaltarget.Window{WeekdayMask: 127, StartMinute: 0, EndMinute: 720}),
				2: scheduleTestConnection(2, "Asia/Shanghai", terminaltarget.Window{WeekdayMask: 127, StartMinute: 720, EndMinute: 1440}),
			},
			leaves: []int{1, 2},
		},
		{
			name: "mixed timezones are reported rather than unioned",
			connections: map[int]DiagnosticsConnection{
				1: scheduleTestConnection(1, "Asia/Shanghai", terminaltarget.Window{WeekdayMask: 127, StartMinute: 0, EndMinute: 720}),
				2: scheduleTestConnection(2, "Europe/Berlin", terminaltarget.Window{WeekdayMask: 127, StartMinute: 720, EndMinute: 1440}),
			},
			leaves:     []int{1, 2},
			wantReason: ScheduleAvailabilityReasonMixedTimezones,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			finding := evaluateScheduleAvailability(scheduleTestGraph(testCase.connections), scheduleTestResult(operation, testCase.leaves...), operation)
			if testCase.wantReason == "" {
				if finding != nil {
					t.Fatalf("expected no finding, got %+v", finding)
				}
				return
			}
			if finding == nil {
				t.Fatalf("expected reason %q, got no finding", testCase.wantReason)
			}
			if finding.reason != testCase.wantReason {
				t.Fatalf("expected reason %q, got %q", testCase.wantReason, finding.reason)
			}
		})
	}
}

// The projection must stay clock-free, so an unconfigured connection yields
// nothing at all rather than a fabricated "closed".
func TestRoutingScheduleProjection(t *testing.T) {
	if routingScheduleProjection(scheduleTestConnection(1, "Asia/Shanghai")) != nil {
		t.Fatal("expected no projection for a connection without windows")
	}
	projection := routingScheduleProjection(scheduleTestConnection(1, "Asia/Shanghai", terminaltarget.Window{WeekdayMask: 31, StartMinute: 540, EndMinute: 1080}))
	if projection == nil || projection.Timezone != "Asia/Shanghai" || len(projection.Windows) != 1 || projection.CoversFullWeek {
		t.Fatalf("unexpected projection: %+v", projection)
	}
}
