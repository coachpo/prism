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
// the Window KPI and every usage-series bucket. It reads only measured
// evidence: the writer-recorded visible-output delivery span and the
// output-token numerator, both guarded by the persisted state, so buffered
// bursts, non-streaming responses, Images, failures, and historical rows
// (state NULL reads unknown) can never enter an average. The > 0 guard keeps
// the division safe if the delivery-span policy ever changes; a measured zero
// stays a genuine zero.
const outputRateTPSSQL = `CASE WHEN output_rate_state = 'measured'
	          AND output_tokens IS NOT NULL AND output_tokens >= 0
	          AND output_delivery_span_ms IS NOT NULL AND output_delivery_span_ms > 0
	     THEN output_tokens * 1000.0 / output_delivery_span_ms END`

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
	Caliber           ScopeCaliber        `json:"caliber"`
	DatasetCoverage   DatasetCoverage     `json:"dataset_coverage"`
	Samples           ScopeSampleCounts   `json:"samples"`
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
	bounds, coverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", "custom", &from, &to, referenceNow)
	if err != nil {
		return DashboardNowResult{}, err
	}
	from, to = bounds.UsageFrom, bounds.UsageTo
	result := DashboardNowResult{
		GeneratedAt:     referenceNow.UTC(),
		Caliber:         CaliberForScope(ScopeIngress),
		DatasetCoverage: DatasetCoverage{UsageRequestEvents: &coverage},
		Rolling: DashboardNowRolling{
			WindowMinutes: rollingMinutes,
			Coverage:      coverage,
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
	result.Samples = ScopeSampleCounts{ObservationCount: result.Rolling.RequestCount, CostMissingCount: result.Rolling.RequestCount}
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1 AND is_enabled = TRUE`, profileID).Scan(&result.EnabledModelCount); err != nil {
		return result, fmt.Errorf("load enabled model count for profile %d: %w", profileID, err)
	}
	return result, nil
}
