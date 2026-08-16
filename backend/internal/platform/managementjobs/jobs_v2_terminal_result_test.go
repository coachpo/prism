package managementjobs

import (
	"testing"
	"time"
)

func TestCancellationScopeFor(t *testing.T) {
	started, manual, automatic := time.Unix(1, 0), "manual", "automatic"
	for _, test := range []struct {
		name, want string
		row        v2RetentionJobRow
	}{
		{"queued manual", cancellationScopeQueuedNoDataChanged, v2RetentionJobRow{ContractVersion: 2, Origin: &manual, CancelRequested: true}},
		{"started then requeued manual", cancellationScopeQueuedNoDataChanged, v2RetentionJobRow{ContractVersion: 2, Origin: &manual, StartedAt: &started, CancelRequested: true}},
		{"started automatic", cancellationScopeAutomaticRemainingStepsOnly, v2RetentionJobRow{ContractVersion: 2, Origin: &automatic, StartedAt: &started, CancelRequested: true}},
	} {
		if got := cancellationScopeFor(test.row); got != test.want {
			t.Errorf("%s: cancellation scope = %q, want %q", test.name, got, test.want)
		}
	}
}
