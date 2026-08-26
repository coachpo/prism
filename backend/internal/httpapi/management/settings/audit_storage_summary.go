package settings

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleGetAuditStorageSummary: bounded logical storage facts + owner
// projections in one shared-fence RR snapshot (SPEC §9.4).
func (s *Service) handleGetAuditStorageSummary(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "settings audit storage summary", func(tx pgx.Tx) (auditStorageSummary, error) {
		now := s.now().UTC()
		source, err := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, "audit_logs", now)
		if err != nil {
			return auditStorageSummary{}, err
		}
		protection, err := auditdomain.LoadAuditFenceMaterializerProjection(r.Context(), tx, now)
		if err != nil {
			return auditStorageSummary{}, err
		}
		var factState struct {
			CurrentGeneration *string
			FactsComplete     bool
			LastFactDay       *time.Time
			GeneratedAt       *time.Time
		}
		if err := tx.QueryRow(r.Context(), `SELECT current_generation, facts_complete, last_fact_day, generated_at
			FROM audit_storage_fact_state WHERE id = 1`).Scan(
			&factState.CurrentGeneration, &factState.FactsComplete, &factState.LastFactDay, &factState.GeneratedAt); err != nil {
			return auditStorageSummary{}, err
		}

		response := auditStorageSummary{
			SourceRevision:  source.SourceRevision,
			GeneratedAt:     now.Format(time.RFC3339),
			RetentionSource: retentionSourceProjectionMap(source),
			AuditProtection: protection,
			SampledDays:     0,
			Precision:       "unavailable",
			Freshness:       "partial",
		}

		if factState.CurrentGeneration != nil && factState.FactsComplete {
			var factCount int64
			var factRevisionMismatch bool
			if err := tx.QueryRow(r.Context(), `SELECT COUNT(*), COALESCE(bool_or(observe_source_revision <> $2), FALSE)
				FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`,
				*factState.CurrentGeneration, source.SourceRevision).Scan(&factCount, &factRevisionMismatch); err != nil {
				return auditStorageSummary{}, err
			}
			if factCount == 0 || factRevisionMismatch {
				reason := "facts_not_ready"
				if factRevisionMismatch {
					reason = "source_revision_mismatch"
				}
				response.StorageFactEvidence = map[string]any{"state": "unavailable", "reason_code": reason}
				return response, nil
			}
			var facts struct {
				TotalRows     int64
				HeaderBytes   int64
				BodyBytes     int64
				SevenDayBytes int64
				DayCount      int
				SevenDayCount int
			}
			windowStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
			windowEnd := windowStart.AddDate(0, 0, 7)
			err := tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(logical_rows),0), COALESCE(SUM(logical_header_bytes),0),
				COALESCE(SUM(logical_body_bytes),0),
				COALESCE(SUM(logical_header_bytes + logical_body_bytes) FILTER (WHERE utc_day >= $2::date AND utc_day < $3::date),0),
				COUNT(*), COUNT(*) FILTER (WHERE utc_day >= $2::date AND utc_day < $3::date)
				FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`, *factState.CurrentGeneration,
				windowStart.Format("2006-01-02"), windowEnd.Format("2006-01-02")).
				Scan(&facts.TotalRows, &facts.HeaderBytes, &facts.BodyBytes, &facts.SevenDayBytes, &facts.DayCount, &facts.SevenDayCount)
			if err == nil {
				total := fmt.Sprintf("%d", facts.TotalRows)
				header := fmt.Sprintf("%d", facts.HeaderBytes)
				body := fmt.Sprintf("%d", facts.BodyBytes)
				response.RetainedRows = &total
				response.LogicalHeaderBytes = &header
				response.LogicalBodyBytes = &body
				response.SampledDays = facts.DayCount
				if facts.SevenDayCount == 7 {
					sevenDay := fmt.Sprintf("%d", facts.SevenDayBytes)
					average := fmt.Sprintf("%d", facts.SevenDayBytes/7)
					response.Last7dLogicalBytesAdded = &sevenDay
					response.DailyAverageLogicalBytes = &average
				}
				response.Precision = "exact"
				response.Freshness = "fresh"
				response.StorageFactEvidence = map[string]any{"state": "bound", "generation": *factState.CurrentGeneration}
			} else {
				response.StorageFactEvidence = map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}
			}
		} else {
			response.StorageFactEvidence = map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}
		}
		return response, nil
	})
	if err != nil {
		writeSettingsInternalError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}
