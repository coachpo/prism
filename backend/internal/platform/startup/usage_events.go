package startup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type usageEventBillingRow struct {
	ID               int
	ProfileID        int
	IngressRequestID string
	BillableFlag     sql.NullBool
	PricedFlag       sql.NullBool
}

type requestLogBillingCandidate struct {
	ID               int
	ProfileID        int
	IngressRequestID string
	AttemptNumber    sql.NullInt32
	CreatedAt        time.Time
	BillableFlag     sql.NullBool
	PricedFlag       sql.NullBool
	UnpricedReason   sql.NullString
}

type usageEventBillingUpdate struct {
	UsageEventID   int
	BillableFlag   bool
	PricedFlag     bool
	UnpricedReason any
}

func (s Service) reconcileUsageRequestEventBillingFields(ctx context.Context, conn *pgx.Conn) (BillingReconciliationResult, error) {
	var pendingRowCount int
	if err := conn.QueryRow(
		ctx,
		`SELECT COUNT(*)
		FROM usage_request_events
		WHERE billable_flag IS NULL OR priced_flag IS NULL`,
	).Scan(&pendingRowCount); err != nil {
		return BillingReconciliationResult{}, fmt.Errorf("count pending usage-event billing rows: %w", err)
	}
	if pendingRowCount == 0 {
		return BillingReconciliationResult{PendingRowCount: 0, Ran: false}, nil
	}

	result := BillingReconciliationResult{
		PendingRowCount: pendingRowCount,
		Ran:             true,
	}

	err := withTransaction(ctx, conn, func(tx pgx.Tx) error {
		usageEventRows, err := loadUsageEventBillingRows(ctx, tx)
		if err != nil {
			return err
		}
		if len(usageEventRows) == 0 {
			return nil
		}

		requestLogCandidates, err := loadRequestLogBillingCandidates(ctx, tx, usageEventRows)
		if err != nil {
			return err
		}

		candidateByKey := map[string][]requestLogBillingCandidate{}
		for _, candidate := range requestLogCandidates {
			key := usageEventKey(candidate.ProfileID, candidate.IngressRequestID)
			candidateByKey[key] = append(candidateByKey[key], candidate)
		}

		updates := make([]usageEventBillingUpdate, 0, pendingRowCount)
		for _, usageEvent := range usageEventRows {
			key := usageEventKey(usageEvent.ProfileID, usageEvent.IngressRequestID)
			candidates := candidateByKey[key]
			if len(candidates) > 0 {
				result.MatchedRequestLogCount++
				if len(candidates) > 1 {
					result.DuplicateCandidateCount++
				}
			} else {
				result.UnmatchedUsageEventCount++
			}

			if usageEvent.BillableFlag.Valid && usageEvent.PricedFlag.Valid {
				continue
			}

			if len(candidates) == 0 {
				updates = append(updates, usageEventBillingUpdate{
					UsageEventID:   usageEvent.ID,
					BillableFlag:   false,
					PricedFlag:     false,
					UnpricedReason: MissingRequestLogBackfillReason,
				})
				continue
			}

			finalCandidate := selectFinalRequestLogCandidate(candidates)
			updates = append(updates, usageEventBillingUpdate{
				UsageEventID:   usageEvent.ID,
				BillableFlag:   finalCandidate.BillableFlag.Valid && finalCandidate.BillableFlag.Bool,
				PricedFlag:     finalCandidate.PricedFlag.Valid && finalCandidate.PricedFlag.Bool,
				UnpricedReason: nullString(finalCandidate.UnpricedReason),
			})
		}

		for _, update := range updates {
			if _, err := tx.Exec(
				ctx,
				`UPDATE usage_request_events
				SET billable_flag = $2, priced_flag = $3, unpriced_reason = $4
				WHERE id = $1`,
				update.UsageEventID,
				update.BillableFlag,
				update.PricedFlag,
				update.UnpricedReason,
			); err != nil {
				return fmt.Errorf("update usage-event billing fields for id %d: %w", update.UsageEventID, err)
			}
		}

		return nil
	})
	if err != nil {
		return BillingReconciliationResult{}, err
	}

	return result, nil
}

func loadUsageEventBillingRows(ctx context.Context, exec queryExecutor) ([]usageEventBillingRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, profile_id, ingress_request_id, billable_flag, priced_flag
		FROM usage_request_events`,
	)
	if err != nil {
		return nil, fmt.Errorf("query usage-event billing rows: %w", err)
	}
	defer rows.Close()

	usageEvents := []usageEventBillingRow{}
	for rows.Next() {
		var row usageEventBillingRow
		if err := rows.Scan(&row.ID, &row.ProfileID, &row.IngressRequestID, &row.BillableFlag, &row.PricedFlag); err != nil {
			return nil, fmt.Errorf("scan usage-event billing row: %w", err)
		}
		usageEvents = append(usageEvents, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage-event billing rows: %w", err)
	}
	return usageEvents, nil
}

func loadRequestLogBillingCandidates(ctx context.Context, exec queryExecutor, usageEvents []usageEventBillingRow) ([]requestLogBillingCandidate, error) {
	if len(usageEvents) == 0 {
		return nil, nil
	}

	ingressIDs := make([]string, 0, len(usageEvents))
	profileSet := map[int]struct{}{}
	ingressSet := map[string]struct{}{}
	usageKeys := map[string]struct{}{}
	for _, usageEvent := range usageEvents {
		usageKeys[usageEventKey(usageEvent.ProfileID, usageEvent.IngressRequestID)] = struct{}{}
		if _, ok := ingressSet[usageEvent.IngressRequestID]; !ok {
			ingressSet[usageEvent.IngressRequestID] = struct{}{}
			ingressIDs = append(ingressIDs, usageEvent.IngressRequestID)
		}
		profileSet[usageEvent.ProfileID] = struct{}{}
	}

	profileIDs := make([]int, 0, len(profileSet))
	for profileID := range profileSet {
		profileIDs = append(profileIDs, profileID)
	}

	rows, err := exec.Query(
		ctx,
		`SELECT
			id,
			profile_id,
			ingress_request_id,
			attempt_number,
			created_at,
			billable_flag,
			priced_flag,
			unpriced_reason
		FROM request_logs
		WHERE ingress_request_id IS NOT NULL
		  AND profile_id = ANY($1)
		  AND ingress_request_id = ANY($2)`,
		toInt32Slice(profileIDs),
		ingressIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query request-log billing candidates: %w", err)
	}
	defer rows.Close()

	candidates := []requestLogBillingCandidate{}
	for rows.Next() {
		var row requestLogBillingCandidate
		if err := rows.Scan(
			&row.ID,
			&row.ProfileID,
			&row.IngressRequestID,
			&row.AttemptNumber,
			&row.CreatedAt,
			&row.BillableFlag,
			&row.PricedFlag,
			&row.UnpricedReason,
		); err != nil {
			return nil, fmt.Errorf("scan request-log billing candidate: %w", err)
		}
		if _, ok := usageKeys[usageEventKey(row.ProfileID, row.IngressRequestID)]; !ok {
			continue
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request-log billing candidates: %w", err)
	}
	return candidates, nil
}

func selectFinalRequestLogCandidate(candidates []requestLogBillingCandidate) requestLogBillingCandidate {
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if compareRequestLogCandidate(candidate, best) > 0 {
			best = candidate
		}
	}
	return best
}

func compareRequestLogCandidate(left requestLogBillingCandidate, right requestLogBillingCandidate) int {
	leftAttempt := int32(-1)
	if left.AttemptNumber.Valid {
		leftAttempt = left.AttemptNumber.Int32
	}
	rightAttempt := int32(-1)
	if right.AttemptNumber.Valid {
		rightAttempt = right.AttemptNumber.Int32
	}
	if leftAttempt != rightAttempt {
		if leftAttempt > rightAttempt {
			return 1
		}
		return -1
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		if left.CreatedAt.After(right.CreatedAt) {
			return 1
		}
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	return 0
}

func usageEventKey(profileID int, ingressRequestID string) string {
	return fmt.Sprintf("%d\x00%s", profileID, ingressRequestID)
}
