package stats

import "time"

const DashboardStatsStaleAfter = 2 * time.Minute

type DashboardSnapshotHealth struct {
	LagSeconds        int64 `json:"lag_seconds"`
	Stale             bool  `json:"stale"`
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
}

type DashboardSnapshotCoverage struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func NewDashboardSnapshotHealth(generatedAt time.Time, referenceNow time.Time) DashboardSnapshotHealth {
	generatedAt = generatedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = referenceNow.UTC()
	}
	lag := referenceNow.UTC().Sub(generatedAt)
	if lag < 0 {
		lag = 0
	}
	return DashboardSnapshotHealth{LagSeconds: int64(lag.Seconds()), Stale: lag > DashboardStatsStaleAfter, StaleAfterSeconds: int64(DashboardStatsStaleAfter.Seconds())}
}
