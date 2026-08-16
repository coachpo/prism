package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestRefreshPublishedPlanningSnapshotsUsesDefaultProfileOnly(t *testing.T) {
	t.Parallel()

	tx := newFrozenDefaultPlanningRefreshFakeTx()
	snapshots, err := refreshPublishedPlanningSnapshots(context.Background(), tx, nil, RefreshRequest{PlanningAll: true}, "")
	if err != nil {
		t.Fatalf("refresh published planning snapshots: %v", err)
	}

	if tx.sawAllProfilesQuery {
		t.Fatal("expected default-only planning refresh to skip listing every profile")
	}
	if len(tx.planningProfileIDs) == 0 {
		t.Fatal("expected planning refresh to query the default profile")
	}
	for _, profileID := range tx.planningProfileIDs {
		if profileID != profiledomain.DefaultProfileID {
			t.Fatalf("expected planning refresh to stay on default profile, saw %d", profileID)
		}
	}
	if _, ok := snapshots[profiledomain.DefaultProfileID]; !ok {
		t.Fatalf("expected default profile planning snapshot, got %+v", snapshots)
	}
	if _, ok := snapshots[42]; ok {
		t.Fatalf("expected non-default profile to stay unplanned, got %+v", snapshots)
	}
}

type frozenDefaultPlanningRefreshFakeTx struct {
	planningProfileIDs  []int
	sawAllProfilesQuery bool
}

func newFrozenDefaultPlanningRefreshFakeTx() *frozenDefaultPlanningRefreshFakeTx {
	return &frozenDefaultPlanningRefreshFakeTx{}
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(query, "FROM model_configs") && strings.Contains(query, "model_configs.loadbalance_strategy_id"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return nil, err
		}
		tx.planningProfileIDs = append(tx.planningProfileIDs, profileID)
		return newRuntimePlanningRows([]any{11, profileID, "openai", "router-openai", sql.NullInt32{}, sql.NullString{}, sql.NullString{}, time.Unix(2, 0).UTC(), true, false}), nil
	case strings.Contains(query, "FROM model_access_targets"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return nil, err
		}
		tx.planningProfileIDs = append(tx.planningProfileIDs, profileID)
		return newRuntimePlanningRows(), nil
	case strings.Contains(query, "FROM loadbalance_strategies"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return nil, err
		}
		tx.planningProfileIDs = append(tx.planningProfileIDs, profileID)
		return newRuntimePlanningRows(), nil
	case strings.Contains(query, "FROM connections") && strings.Contains(query, "JOIN endpoints"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return nil, err
		}
		tx.planningProfileIDs = append(tx.planningProfileIDs, profileID)
		return newRuntimePlanningRows(), nil
	case strings.Contains(query, "FROM header_blocklist_rules"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return nil, err
		}
		tx.planningProfileIDs = append(tx.planningProfileIDs, profileID)
		return newRuntimePlanningRows(), nil
	case strings.Contains(query, "FROM profiles") && strings.Contains(query, "ORDER BY id ASC") && strings.Contains(query, "deleted_at IS NULL"):
		tx.sawAllProfilesQuery = true
		return nil, fmt.Errorf("unexpected all-profiles planning query: %s", query)
	default:
		return nil, fmt.Errorf("unexpected planning refresh query: %s", query)
	}
}

func (tx *frozenDefaultPlanningRefreshFakeTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "FROM profiles") && strings.Contains(query, "WHERE id = $1") && strings.Contains(query, "deleted_at IS NULL"):
		profileID, err := frozenDefaultProfileIDArg(args)
		if err != nil {
			return runtimePlanningRow{err: err}
		}
		if profileID != profiledomain.DefaultProfileID {
			return runtimePlanningRow{err: fmt.Errorf("unexpected profile lookup %d", profileID)}
		}
		now := time.Unix(1, 0).UTC()
		return runtimePlanningRow{values: []any{profiledomain.DefaultProfileID, profiledomain.DefaultProfileName, sql.NullString{String: profiledomain.DefaultProfileDescription, Valid: true}, true, true, true, 1, sql.NullTime{}, now, now}}
	case strings.Contains(query, "FROM runtime_cache_generations"):
		return runtimePlanningRow{values: []any{int64(1)}}
	case strings.Contains(query, "FROM app_auth_settings"):
		return runtimePlanningRow{values: []any{false}}
	case strings.Contains(query, "FROM user_settings"):
		return runtimePlanningRow{values: []any{"USD", "$", sql.NullInt64{Int64: 1, Valid: true}}}
	default:
		return runtimePlanningRow{err: fmt.Errorf("unexpected planning refresh query row: %s", query)}
	}
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Commit(context.Context) error {
	return errors.New("not implemented")
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Rollback(context.Context) error {
	return errors.New("not implemented")
}

func (tx *frozenDefaultPlanningRefreshFakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (tx *frozenDefaultPlanningRefreshFakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *frozenDefaultPlanningRefreshFakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}

func (tx *frozenDefaultPlanningRefreshFakeTx) Conn() *pgx.Conn {
	return nil
}

func frozenDefaultProfileIDArg(args []any) (int, error) {
	if len(args) == 0 {
		return 0, errors.New("missing profile id argument")
	}
	profileID, ok := args[0].(int)
	if !ok {
		return 0, fmt.Errorf("unexpected profile id argument type %T", args[0])
	}
	return profileID, nil
}
