package realtime

import (
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

type DashboardSnapshotMessage struct {
	Type      string                        `json:"type"`
	ProfileID int                           `json:"profile_id"`
	Snapshot  statsdomain.DashboardSnapshot `json:"snapshot"`
}

type DashboardActivityMessage struct {
	Type              string                                       `json:"type"`
	ProfileID         int                                          `json:"profile_id"`
	ActivityWatermark statsdomain.DashboardRecentActivityWatermark `json:"activity_watermark"`
	Activity          statsdomain.DashboardRecentActivityItem      `json:"activity"`
}

type AnalyticsSnapshotMessage struct {
	Type                                string                                       `json:"type"`
	Channel                             string                                       `json:"channel"`
	ProfileID                           int                                          `json:"profile_id"`
	Preset                              string                                       `json:"preset"`
	Sequence                            int64                                        `json:"sequence"`
	GeneratedAt                         time.Time                                    `json:"generated_at"`
	Snapshot                            statsdomain.UsageSnapshotResponse            `json:"snapshot"`
	EndpointModelStatisticsByEndpointID map[string][]statsdomain.UsageModelStatistic `json:"endpoint_model_statistics_by_endpoint_id"`
}

type AnalyticsErrorMessage struct {
	Type      string  `json:"type"`
	Channel   string  `json:"channel"`
	ProfileID *int    `json:"profile_id,omitempty"`
	Preset    *string `json:"preset,omitempty"`
	Code      string  `json:"code"`
	Message   string  `json:"message"`
}
