package stats

import (
	"context"
	"fmt"
	"time"
)

// Observe read models: single-statement PostgreSQL aggregates over the
// finalized usage table. The outcome classifier is expressed as one shared
// SQL CASE (mirroring ClassifyOutcomeDetail); pricing facts are read from the
// persisted four-state columns, never re-derived.

// outcomeDetailSQL is the single shared classifier expression.
const outcomeDetailSQL = `CASE
	WHEN status_code NOT BETWEEN 200 AND 299 THEN 'http_error'
	WHEN stream_outcome = 'client_disconnected' THEN 'client_disconnected'
	WHEN stream_outcome IS NULL OR stream_outcome IN ('', 'not_streaming', 'completed') THEN 'completed'
	ELSE 'stream_error'
END`

const usageWindowPredicate = `profile_id = $1 AND created_at >= $2 AND created_at < $3`

// outputRateTPSSQL is the single per-request output-rate expression shared by
// the Window KPI and every usage-series bucket. It intentionally does not add
// an outcome filter: measurability is exactly the three non-null fields plus a
// positive post-TTFT duration.
const outputRateTPSSQL = `CASE WHEN output_tokens IS NOT NULL AND ttft_ms IS NOT NULL AND completion_duration_ms IS NOT NULL
	          AND completion_duration_ms - ttft_ms > 0
	     THEN output_tokens * 1000.0 / (completion_duration_ms - ttft_ms) END`

func stringPointer(value string) *string {
	resolved := value
	return &resolved
}

// LoadReportCurrencyPreferences exposes the profile report currency read for
// management handlers.
func LoadReportCurrencyPreferences(ctx context.Context, exec queryExecutor, profileID int) (string, string, error) {
	return loadReportCurrencyPreferences(ctx, exec, profileID)
}

// ---- Dashboard Now read model ----

type DashboardNowResult struct {
	GeneratedAt       time.Time           `json:"generated_at"`
	Health            DashboardNowHealth  `json:"health"`
	Rolling           DashboardNowRolling `json:"rolling"`
	EnabledModelCount int                 `json:"enabled_model_count"`
}

type DashboardNowHealth struct {
	Stale      bool `json:"stale"`
	CacheLagMS *int `json:"cache_lag_ms"`
}

type DashboardNowRolling struct {
	WindowMinutes         int      `json:"window_minutes"`
	Coverage              Coverage `json:"coverage"`
	RequestCount          int      `json:"request_count"`
	TokenSampleCount      int      `json:"token_sample_count"`
	TokenCoverageComplete bool     `json:"token_coverage_complete"`
	TokenCount            *int64   `json:"token_count"`
	RPM                   *float64 `json:"rpm"`
	TPM                   *float64 `json:"tpm"`
}

// LoadDashboardNow computes the 30-minute rolling Now strip in one statement
// plus the configured model count.
func LoadDashboardNow(ctx context.Context, exec queryExecutor, profileID int, referenceNow time.Time, rollingMinutes int) (DashboardNowResult, error) {
	if rollingMinutes <= 0 {
		rollingMinutes = 30
	}
	from := referenceNow.UTC().Add(-time.Duration(rollingMinutes) * time.Minute)
	to := referenceNow.UTC()
	result := DashboardNowResult{
		GeneratedAt: referenceNow.UTC(),
		Rolling: DashboardNowRolling{
			WindowMinutes: rollingMinutes,
			Coverage: Coverage{
				RequestedPreset: "rolling",
				FromTime:        from,
				ToTime:          to,
				Source:          "raw",
				Complete:        true,
				Precision:       &CoveragePrecision{TTFT: "exact", OutputRate: "exact"},
			},
		},
	}
	var tokenCount *int64
	var rpm *float64
	row := exec.QueryRow(ctx, `
SELECT
	COUNT(*)::int,
	COUNT(total_tokens)::int,
	SUM(total_tokens),
	COUNT(*)::float / $4
FROM usage_request_events
WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`,
		profileID, from, to, float64(rollingMinutes))
	if err := row.Scan(&result.Rolling.RequestCount, &result.Rolling.TokenSampleCount, &tokenCount, &rpm); err != nil {
		return result, fmt.Errorf("load dashboard now rolling for profile %d: %w", profileID, err)
	}
	result.Rolling.TokenCount = tokenCount
	result.Rolling.RPM = rpm
	if tokenCount != nil {
		value := float64(*tokenCount) / float64(rollingMinutes)
		result.Rolling.TPM = &value
	}
	result.Rolling.TokenCoverageComplete = result.Rolling.TokenSampleCount == result.Rolling.RequestCount
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1 AND is_enabled = TRUE`, profileID).Scan(&result.EnabledModelCount); err != nil {
		return result, fmt.Errorf("load enabled model count for profile %d: %w", profileID, err)
	}
	return result, nil
}
