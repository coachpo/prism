package stats

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollingWindowMinutes = 30

type HTTPError struct {
	StatusCode int
	Detail     string
	Code       string
	Details    map[string]any
}

func (err *HTTPError) Error() string {
	return err.Detail
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type endpointRecord struct {
	ID      int
	Name    *string
	BaseURL *string
}

type compiledUserAgentRule struct {
	ID         int
	Name       string
	RawPattern string
	Pattern    *regexp.Regexp
}

type usageEventRecord struct {
	ID                             int
	CreatedAt                      time.Time
	ProfileID                      int
	IngressRequestID               string
	ModelID                        string
	ResolvedTargetModelID          *string
	APIFamily                      string
	EndpointID                     *int
	EndpointLabelSnapshot          string
	ConnectionID                   *int
	ProxyAPIKeyID                  *int
	ProxyAPIKeyNameSnapshot        *string
	StatusCode                     int
	SuccessFlag                    bool
	BillableFlag                   bool
	PricedFlag                     bool
	UnpricedReason                 *string
	InputTokens                    int
	OutputTokens                   int
	HasOutputTokens                bool
	TotalTokens                    int
	CacheReadInputTokens           int
	CacheCreationInputTokens       int
	ReasoningTokens                int
	TotalCostUserCurrencyMicros    int64
	HasTotalCostUserCurrencyMicros bool
	AttemptCount                   int
	RequestPath                    string
	ResponseTimeMS                 *int
	TTFTMS                         *int
	CompletionDurationMS           *int
	CurrentModelLabel              *string
	CurrentEndpointName            *string
	CurrentEndpointBaseURL         *string
	CurrentProxyAPIKeyName         *string
	CurrentProxyAPIKeyPrefix       *string
}

type snapshotEvent struct {
	APIFamily                string
	AttemptCount             int
	BillableFlag             bool
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	ConnectionID             *int
	CreatedAt                time.Time
	EndpointID               *int
	EndpointLabel            string
	IngressRequestID         string
	InputTokens              int
	ModelID                  string
	ModelLabel               string
	OutputTokens             int
	PricedFlag               bool
	ProxyAPIKeyID            *int
	ProxyAPIKeyLabel         *string
	ProxyAPIKeyStatsLabel    string
	ProxyAPIKeyPrefix        *string
	ReasoningTokens          int
	RequestPath              string
	ResolvedTargetModelID    *string
	StatusCode               int
	SuccessFlag              bool
	ResponseTimeMS           *int
	TTFTMS                   *int
	CompletionDurationMS     *int
	HasOutputTokens          bool
	TotalCostMicros          int64
	TotalTokens              int
}

func normalizeTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	resolved := value.UTC()
	return &resolved
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func roundFloat(value float64, places int) float64 {
	factor := math.Pow10(places)
	return math.Round(value*factor) / factor
}

func successRate(successCount int, totalCount int) float64 {
	if totalCount <= 0 {
		return 0
	}
	return roundFloat((float64(successCount)/float64(totalCount))*100, 2)
}

func percentileContInt(values []int, percentile float64) *int {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	rank := float64(len(ordered)-1) * percentile
	lowerIndex := int(math.Floor(rank))
	upperIndex := int(math.Ceil(rank))
	lowerValue := float64(ordered[lowerIndex])
	upperValue := float64(ordered[upperIndex])
	interpolated := lowerValue + (upperValue-lowerValue)*(rank-float64(lowerIndex))
	resolved := int(math.Round(interpolated))
	return &resolved
}

func effectiveWindowStart(startAt *time.Time, endAt time.Time, events []snapshotEvent) time.Time {
	if startAt != nil {
		return startAt.UTC()
	}
	if len(events) == 0 {
		return endAt.UTC()
	}
	minValue := events[0].CreatedAt.UTC()
	for _, event := range events[1:] {
		if event.CreatedAt.Before(minValue) {
			minValue = event.CreatedAt.UTC()
		}
	}
	return minValue
}

func resolveTimePreset(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (*time.Time, *time.Time) {
	normalizedPreset := strings.TrimSpace(strings.ToLower(preset))
	if normalizedPreset == "" || normalizedPreset == "custom" {
		return normalizeTimePointer(fromTime), normalizeTimePointer(toTime)
	}
	referenceTime := referenceNow.UTC()
	if toTime != nil {
		referenceTime = toTime.UTC()
	}
	switch normalizedPreset {
	case "1h":
		from := referenceTime.Add(-time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "6h":
		from := referenceTime.Add(-6 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "7h":
		from := referenceTime.Add(-7 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "24h":
		from := referenceTime.Add(-24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "last_7_days", "7d":
		from := referenceTime.Add(-7 * 24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "last_30_days", "30d":
		from := referenceTime.Add(-30 * 24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "all":
		return nil, normalizeTimePointer(toTime)
	default:
		return normalizeTimePointer(fromTime), normalizeTimePointer(toTime)
	}
}

func loadReportCurrencyPreferences(ctx context.Context, exec queryExecutor, profileID int) (string, string, error) {
	var code string
	var symbol string
	err := exec.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&code, &symbol)
	if err == nil {
		return code, symbol, nil
	}
	if err == pgx.ErrNoRows {
		return "USD", "$", nil
	}
	return "", "", fmt.Errorf("load report currency preferences for profile %d: %w", profileID, err)
}

func loadCurrentEndpoints(ctx context.Context, exec queryExecutor, profileID int) ([]endpointRecord, map[int]endpointRecord, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, base_url FROM endpoints WHERE profile_id = $1 ORDER BY lower(name) ASC, name ASC, id ASC`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("query endpoints for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]endpointRecord, 0)
	itemsByID := map[int]endpointRecord{}
	for rows.Next() {
		var name sql.NullString
		var baseURL sql.NullString
		var item endpointRecord
		if err := rows.Scan(&item.ID, &name, &baseURL); err != nil {
			return nil, nil, fmt.Errorf("scan endpoint record: %w", err)
		}
		item.Name = nullableString(name)
		item.BaseURL = nullableString(baseURL)
		items = append(items, item)
		itemsByID[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate endpoints for profile %d: %w", profileID, err)
	}
	return items, itemsByID, nil
}

func resolveEndpointLabel(name *string, baseURL *string, historicalBaseURL *string, endpointID *int, unknownLabel string) string {
	if name != nil && strings.TrimSpace(*name) != "" {
		return strings.TrimSpace(*name)
	}
	if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
		return strings.TrimSpace(*baseURL)
	}
	if historicalBaseURL != nil && strings.TrimSpace(*historicalBaseURL) != "" {
		return strings.TrimSpace(*historicalBaseURL)
	}
	if endpointID != nil {
		return fmt.Sprintf("Endpoint %d", *endpointID)
	}
	return unknownLabel
}

func usageEventEndpointLabel(record usageEventRecord) string {
	if label := strings.TrimSpace(record.EndpointLabelSnapshot); label != "" {
		return label
	}
	return "Unknown Endpoint"
}

func loadCompiledUserAgentRules(ctx context.Context, exec queryExecutor, profileID int) ([]compiledUserAgentRule, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, pattern FROM user_agent_client_rules WHERE enabled = TRUE AND (profile_id = $1 OR is_system = TRUE) ORDER BY is_system ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query user-agent client rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]compiledUserAgentRule, 0)
	for rows.Next() {
		var id int
		var name string
		var pattern string
		if err := rows.Scan(&id, &name, &pattern); err != nil {
			return nil, fmt.Errorf("scan user-agent client rule: %w", err)
		}
		compiled, compileErr := regexp.Compile("(?i)" + pattern)
		if compileErr != nil {
			continue
		}
		items = append(items, compiledUserAgentRule{ID: id, Name: name, RawPattern: pattern, Pattern: compiled})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user-agent client rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadCompiledUserAgentRuleByID(ctx context.Context, exec queryExecutor, profileID int, ruleID int) (compiledUserAgentRule, bool, error) {
	var id int
	var name string
	var pattern string
	err := exec.QueryRow(ctx, `SELECT id, name, pattern FROM user_agent_client_rules WHERE id = $1 AND enabled = TRUE AND (profile_id = $2 OR is_system = TRUE)`, ruleID, profileID).Scan(&id, &name, &pattern)
	if err == pgx.ErrNoRows {
		return compiledUserAgentRule{}, false, nil
	}
	if err != nil {
		return compiledUserAgentRule{}, false, fmt.Errorf("load user-agent client rule %d for profile %d: %w", ruleID, profileID, err)
	}
	compiled, compileErr := regexp.Compile("(?i)" + pattern)
	if compileErr != nil {
		return compiledUserAgentRule{}, false, fmt.Errorf("compile user-agent client rule %d: %w", ruleID, compileErr)
	}
	return compiledUserAgentRule{ID: id, Name: name, RawPattern: pattern, Pattern: compiled}, true, nil
}

func classifyUserAgentDisplay(userAgent *string, rules []compiledUserAgentRule) *string {
	if userAgent == nil {
		return nil
	}
	for _, rule := range rules {
		if rule.Pattern.MatchString(*userAgent) {
			resolved := rule.Name
			return &resolved
		}
	}
	resolved := *userAgent
	return &resolved
}

func userAgentOverridden(callerUserAgent *string, upstreamUserAgent *string) bool {
	if callerUserAgent == nil && upstreamUserAgent == nil {
		return false
	}
	if callerUserAgent == nil || upstreamUserAgent == nil {
		return true
	}
	return *callerUserAgent != *upstreamUserAgent
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	resolved := value.Bool
	return &resolved
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Int64
	return &resolved
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func requestOutputRateTPS(outputTokens int, hasOutputTokens bool, ttftMS *int, completionDurationMS *int) *float64 {
	if !hasOutputTokens || ttftMS == nil || completionDurationMS == nil {
		return nil
	}
	postTTFT := *completionDurationMS - *ttftMS
	if postTTFT <= 0 {
		return nil
	}
	resolved := roundFloat((float64(outputTokens)*1000)/float64(postTTFT), 2)
	return &resolved
}

func bucketFloor(value time.Time, granularity string) time.Time {
	normalized := value.UTC()
	switch granularity {
	case "hour":
		return time.Date(normalized.Year(), normalized.Month(), normalized.Day(), normalized.Hour(), 0, 0, 0, time.UTC)
	default:
		return time.Date(normalized.Year(), normalized.Month(), normalized.Day(), 0, 0, 0, 0, time.UTC)
	}
}

func bucketStep(granularity string) time.Duration {
	if granularity == "hour" {
		return time.Hour
	}
	return 24 * time.Hour
}

func bucketMinutes(granularity string) float64 {
	if granularity == "hour" {
		return 60
	}
	return 1440
}

func bucketRange(startAt *time.Time, endAt time.Time, eventTimes []time.Time, granularity string) []time.Time {
	var current time.Time
	if startAt == nil {
		if len(eventTimes) > 0 {
			minValue := eventTimes[0].UTC()
			for _, candidate := range eventTimes[1:] {
				if candidate.Before(minValue) {
					minValue = candidate.UTC()
				}
			}
			current = bucketFloor(minValue, granularity)
		} else {
			current = bucketFloor(endAt, granularity)
		}
	} else {
		current = bucketFloor(startAt.UTC(), granularity)
	}
	endBucket := bucketFloor(endAt, granularity)
	step := bucketStep(granularity)
	buckets := make([]time.Time, 0)
	for !current.After(endBucket) {
		buckets = append(buckets, current)
		current = current.Add(step)
	}
	return buckets
}

func timeSliceFromEvents(events []snapshotEvent) []time.Time {
	items := make([]time.Time, 0, len(events))
	for _, event := range events {
		items = append(items, event.CreatedAt)
	}
	return items
}
