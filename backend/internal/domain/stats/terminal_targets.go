package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// TerminalTargetStatisticsParams scopes the bounded Terminal Target drill-down
// for one endpoint (SPEC: expanding an Endpoint row lazily loads Terminal
// Target detail; unexpanded rows never run high-cardinality queries).
type TerminalTargetStatisticsParams struct {
	ProfileID      int
	EndpointID     int
	Preset         string
	FromTime       *time.Time
	ToTime         *time.Time
	CostSegmentKey string
	Limit          int
	Offset         int
	ReferenceNow   time.Time
}

// TerminalTargetStatistic is one connection (Terminal Target) row of the
// bounded endpoint drill-down. HTTP failure, final failure, client
// disconnects, TTFT, output rate, segment spend, the pricing four states and
// four reasons, coverage, and recorded ban/admission events all use the same
// definitions as the model/endpoint tables (OB-28..33).
type TerminalTargetStatistic struct {
	ConnectionID            int                  `json:"connection_id"`
	ConnectionLabel         string               `json:"connection_label"`
	RequestCount            int                  `json:"request_count"`
	HTTPSuccessCount        int                  `json:"http_success_count"`
	HTTPFailedCount         int                  `json:"http_failed_count"`
	FinalFailedCount        int                  `json:"final_failed_count"`
	ClientDisconnectedCount int                  `json:"client_disconnected_count"`
	P50TTFTMS               *int                 `json:"p50_ttft_ms"`
	P95TTFTMS               *int                 `json:"p95_ttft_ms"`
	AvgOutputRateTPS        *float64             `json:"avg_output_rate_tps"`
	TotalTokens             int                  `json:"total_tokens"`
	TotalCostMicros         int64                `json:"total_cost_micros"`
	PricingStatusCounts     PricingStatusCounts  `json:"pricing_status_counts"`
	UnpricedReasonCounts    UnpricedReasonCounts `json:"unpriced_reason_counts"`
	Coverage                QueryCoverage        `json:"coverage"`
	BanEventCount           int                  `json:"ban_event_count"`
	AdmissionRejectionCount int                  `json:"admission_rejection_count"`
	EventCoverageComplete   bool                 `json:"event_coverage_complete"`
}

type PricingStatusCounts struct {
	Priced     int `json:"priced"`
	Unpriced   int `json:"unpriced"`
	Ineligible int `json:"ineligible"`
	Unknown    int `json:"unknown"`
}

type TerminalTargetStatisticsResponse struct {
	Items       []TerminalTargetStatistic `json:"items"`
	Total       int                       `json:"total"`
	Limit       int                       `json:"limit"`
	Offset      int                       `json:"offset"`
	Coverage    QueryCoverage             `json:"coverage"`
	GeneratedAt time.Time                 `json:"generated_at"`
}

type terminalTargetAggregate struct {
	connectionID            int
	connectionLabel         string
	requestCount            int
	httpSuccess             int
	httpFailed              int
	finalFailed             int
	clientDisconnect        int
	ttftValues              []int
	outputRateSum           float64
	eligibleRates           int
	totalTokens             int
	totalCostMicros         int64
	pricingStatus           map[string]int
	unpricedReasons         map[string]int
	minCreatedAt            *time.Time
	maxCreatedAt            *time.Time
	banEventCount           int
	admissionRejectionCount int
}

// GetEndpointTerminalTargetStatistics loads the bounded Terminal Target
// drill-down for one endpoint in a single read snapshot: the usage-event
// aggregation and the loadbalance event counts share the same query window.
func GetEndpointTerminalTargetStatistics(ctx context.Context, exec queryExecutor, params TerminalTargetStatisticsParams) (TerminalTargetStatisticsResponse, error) {
	endpointExists, historicalExists, err := endpointOrHistoricalUsageExists(ctx, exec, params.ProfileID, params.EndpointID)
	if err != nil {
		return TerminalTargetStatisticsResponse{}, err
	}
	if !endpointExists && !historicalExists {
		return TerminalTargetStatisticsResponse{}, &HTTPError{StatusCode: 404, Detail: "Endpoint not found"}
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	startAt, endAt := resolveTimePreset(preset, params.FromTime, params.ToTime, params.ReferenceNow.UTC())
	if endAt == nil {
		resolvedEnd := params.ReferenceNow.UTC()
		endAt = &resolvedEnd
	}
	if startAt == nil {
		resolvedStart := endAt.Add(-time.Hour)
		startAt = &resolvedStart
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	aggregates, err := loadTerminalTargetAggregates(ctx, exec, params.ProfileID, params.EndpointID, startAt, endAt, params.CostSegmentKey)
	if err != nil {
		return TerminalTargetStatisticsResponse{}, err
	}
	items := make([]TerminalTargetStatistic, 0, len(aggregates))
	total := len(aggregates)
	sort.Slice(aggregates, func(i, j int) bool {
		if aggregates[i].requestCount != aggregates[j].requestCount {
			return aggregates[i].requestCount > aggregates[j].requestCount
		}
		return aggregates[i].connectionID < aggregates[j].connectionID
	})
	page := aggregates
	if offset < len(page) {
		page = page[offset:]
	}
	if len(page) > limit {
		page = page[:limit]
	}
	now := params.ReferenceNow.UTC()
	coverage := KnownCoverage(*startAt, *endAt, *startAt, *endAt, nil, len(page), "terminal-targets-v1:"+now.Format("20060102T150405"))
	for _, aggregate := range page {
		items = append(items, terminalTargetStatisticFromAggregate(aggregate, coverage))
	}
	coverage.Precision = &QueryCoveragePrecision{RowCount: len(page)}
	return TerminalTargetStatisticsResponse{
		Items:       items,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
		Coverage:    coverage,
		GeneratedAt: now,
	}, nil
}

func terminalTargetStatisticFromAggregate(aggregate *terminalTargetAggregate, coverage QueryCoverage) TerminalTargetStatistic {
	p50, p95 := percentileTTFT(aggregate.ttftValues)
	var avgOutputRate *float64
	if aggregate.eligibleRates > 0 {
		resolved := aggregate.outputRateSum / float64(aggregate.eligibleRates)
		avgOutputRate = &resolved
	}
	return TerminalTargetStatistic{
		ConnectionID:            aggregate.connectionID,
		ConnectionLabel:         aggregate.connectionLabel,
		RequestCount:            aggregate.requestCount,
		HTTPSuccessCount:        aggregate.httpSuccess,
		HTTPFailedCount:         aggregate.httpFailed,
		FinalFailedCount:        aggregate.finalFailed,
		ClientDisconnectedCount: aggregate.clientDisconnect,
		P50TTFTMS:               p50,
		P95TTFTMS:               p95,
		AvgOutputRateTPS:        avgOutputRate,
		TotalTokens:             aggregate.totalTokens,
		TotalCostMicros:         aggregate.totalCostMicros,
		PricingStatusCounts: PricingStatusCounts{
			Priced:     aggregate.pricingStatus["priced"],
			Unpriced:   aggregate.pricingStatus["unpriced"],
			Ineligible: aggregate.pricingStatus["ineligible"],
			Unknown:    aggregate.pricingStatus["unknown"],
		},
		UnpricedReasonCounts: UnpricedReasonCounts{
			PRICING_DISABLED:         aggregate.unpricedReasons[UnpricedReasonPricingDisabled],
			MISSING_TOKEN_USAGE:      aggregate.unpricedReasons[UnpricedReasonMissingTokenUsage],
			STREAM_USAGE_UNAVAILABLE: aggregate.unpricedReasons[UnpricedReasonStreamUsageUnavailable],
			MISSING_PRICE_DATA:       aggregate.unpricedReasons[UnpricedReasonMissingPriceData],
		},
		Coverage:                coverage,
		BanEventCount:           aggregate.banEventCount,
		AdmissionRejectionCount: aggregate.admissionRejectionCount,
	}
}

func loadTerminalTargetAggregates(ctx context.Context, exec queryExecutor, profileID int, endpointID int, startAt *time.Time, endAt *time.Time, costSegmentKey string) ([]*terminalTargetAggregate, error) {
	rows, err := exec.Query(ctx, `SELECT
		COALESCE(usage_request_events.connection_id, 0),
		COALESCE(NULLIF(connections.name, ''), endpoints.name, usage_request_events.endpoint_label_snapshot, 'Terminal Target'),
		usage_request_events.status_code,
		usage_request_events.success_flag,
		usage_request_events.stream_outcome,
		COALESCE(usage_request_events.pricing_status, 'unknown'),
		COALESCE(usage_request_events.unpriced_reason, ''),
		usage_request_events.ttft_ms,
		COALESCE(usage_request_events.output_tokens, 0),
		COALESCE(usage_request_events.total_tokens, 0),
		COALESCE(usage_request_events.total_cost_user_currency_micros, 0),
		usage_request_events.completion_duration_ms,
		usage_request_events.created_at,
		usage_request_events.endpoint_id
		FROM usage_request_events
		LEFT JOIN connections ON connections.id = usage_request_events.connection_id AND connections.profile_id = usage_request_events.profile_id
		LEFT JOIN endpoints ON endpoints.id = usage_request_events.endpoint_id AND endpoints.profile_id = usage_request_events.profile_id
		WHERE usage_request_events.profile_id = $1 AND usage_request_events.endpoint_id = $2 AND usage_request_events.created_at >= $3 AND usage_request_events.created_at < $4`,
		profileID, endpointID, startAt, endAt)
	if err != nil {
		return nil, fmt.Errorf("query terminal-target usage events: %w", err)
	}
	defer rows.Close()
	aggregates := map[int]*terminalTargetAggregate{}
	var order []int
	for rows.Next() {
		var connectionID int
		var connectionLabel string
		var statusCode int
		var successFlag bool
		var streamOutcome string
		var pricingStatus string
		var unpricedReason string
		var ttftMS sql.NullInt32
		var outputTokens int
		var totalTokens int
		var totalCostMicros int64
		var completionDurationMS sql.NullInt32
		var createdAt time.Time
		var rowEndpointID sql.NullInt32
		if err := rows.Scan(&connectionID, &connectionLabel, &statusCode, &successFlag, &streamOutcome, &pricingStatus, &unpricedReason, &ttftMS, &outputTokens, &totalTokens, &totalCostMicros, &completionDurationMS, &createdAt, &rowEndpointID); err != nil {
			return nil, fmt.Errorf("scan terminal-target usage event: %w", err)
		}
		aggregate, ok := aggregates[connectionID]
		if !ok {
			aggregates[connectionID] = &terminalTargetAggregate{
				connectionID:    connectionID,
				connectionLabel: connectionLabel,
				pricingStatus:   map[string]int{},
				unpricedReasons: map[string]int{},
			}
			aggregate = aggregates[connectionID]
			order = append(order, connectionID)
		}
		aggregate.requestCount++
		if successFlag {
			aggregate.httpSuccess++
		} else {
			aggregate.httpFailed++
		}
		switch classifyOutcomeDetail(statusCode, streamOutcome) {
		case "http_error", "stream_error":
			aggregate.finalFailed++
		case "client_disconnected":
			aggregate.clientDisconnect++
		}
		if ttftMS.Valid {
			aggregate.ttftValues = append(aggregate.ttftValues, int(ttftMS.Int32))
		}
		if outputRate := requestOutputRateTPS(outputTokens, true, nullableIntPointer(ttftMS), nullableIntPointer(completionDurationMS)); outputRate != nil {
			aggregate.outputRateSum += *outputRate
			aggregate.eligibleRates++
		}
		aggregate.totalTokens += totalTokens
		aggregate.totalCostMicros += totalCostMicros
		status := "unknown"
		if pricingStatus != "" {
			status = pricingStatus
		}
		aggregate.pricingStatus[status]++
		if unpricedReason != "" {
			aggregate.unpricedReasons[unpricedReason]++
		}
		aggregate.minCreatedAt = minTimePointer(aggregate.minCreatedAt, createdAt)
		aggregate.maxCreatedAt = maxTimePointer(aggregate.maxCreatedAt, createdAt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal-target usage events: %w", err)
	}
	resolved := make([]*terminalTargetAggregate, 0, len(aggregates))
	for _, id := range order {
		resolved = append(resolved, aggregates[id])
	}
	// Loadbalance ban/admission events per connection in the same window.
	if err := attachTerminalTargetEventCounts(ctx, exec, profileID, endpointID, startAt, endAt, aggregates); err != nil {
		return nil, err
	}
	return resolved, nil
}

func attachTerminalTargetEventCounts(ctx context.Context, exec queryExecutor, profileID int, endpointID int, startAt *time.Time, endAt *time.Time, aggregates map[int]*terminalTargetAggregate) error {
	rows, err := exec.Query(ctx, `SELECT connection_id, event_type, COUNT(*) FROM loadbalance_events WHERE profile_id = $1 AND endpoint_id = $2 AND created_at >= $3 AND created_at < $4 GROUP BY connection_id, event_type`, profileID, endpointID, startAt, endAt)
	if err != nil {
		return fmt.Errorf("query terminal-target loadbalance events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var connectionID sql.NullInt32
		var eventType string
		var count int
		if err := rows.Scan(&connectionID, &eventType, &count); err != nil {
			return fmt.Errorf("scan terminal-target event count: %w", err)
		}
		if !connectionID.Valid {
			continue
		}
		aggregate, ok := aggregates[int(connectionID.Int32)]
		if !ok {
			continue
		}
		switch strings.TrimSpace(eventType) {
		case "banned":
			aggregate.banEventCount += count
		case "admission_rejected":
			aggregate.admissionRejectionCount += count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate terminal-target event counts: %w", err)
	}
	return nil
}

type sqlNullString struct {
	String string
	Valid  bool
}

func nullableIntPointer(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func classifyOutcomeDetail(statusCode int, streamOutcome string) string {
	if statusCode < 200 || statusCode > 299 {
		return "http_error"
	}
	switch streamOutcome {
	case "client_disconnected":
		return "client_disconnected"
	case "", "not_streaming", "completed":
		return "completed"
	default:
		return "stream_error"
	}
}

func percentileTTFT(values []int) (*int, *int) {
	if len(values) == 0 {
		return nil, nil
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	p50 := sorted[(len(sorted)-1)/2]
	p95Index := int(float64(len(sorted)-1) * 0.95)
	p95 := sorted[p95Index]
	return &p50, &p95
}

func minTimePointer(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.Before(*current) {
		resolved := candidate
		return &resolved
	}
	return current
}

func maxTimePointer(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.After(*current) {
		resolved := candidate
		return &resolved
	}
	return current
}

var _ = pgx.ErrNoRows
