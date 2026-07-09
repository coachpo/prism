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

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

func TestBuildPlanningSnapshotFreezesRoutingAssemblyContract(t *testing.T) {
	const profileID = 42
	const secretKey = "runtime-snapshot-contract-key"
	encryptedAPIKey, err := endpointdomain.EncryptSecret("sk-runtime-contract", secretKey, func() time.Time {
		return time.Unix(1, 0).UTC()
	})
	if err != nil {
		t.Fatalf("encrypt endpoint key: %v", err)
	}

	tx := newRuntimePlanningSnapshotFakeTx(encryptedAPIKey)
	snapshot, err := buildPlanningSnapshot(context.Background(), tx, profileID, secretKey)
	if err != nil {
		t.Fatalf("build planning snapshot: %v", err)
	}

	model, ok := snapshot.ModelsByID["router-openai"]
	if !ok {
		t.Fatalf("expected router-openai model in snapshot, got %+v", snapshot.ModelsByID)
	}
	if model.ID != 11 || model.ProfileID != profileID || model.APIFamily != "openai" {
		t.Fatalf("unexpected model identity: %+v", model)
	}
	if !model.CreatedAt.Equal(time.Unix(2, 0).UTC()) {
		t.Fatalf("expected model created_at to survive snapshot assembly, got %+v", model.CreatedAt)
	}
	assertRuntimeIntPtr(t, model.LoadbalanceStrategyID, 303, "model strategy")

	strategy, ok := snapshot.StrategiesByModelID[model.ID]
	if !ok || strategy.ID != 303 || strategy.LegacyStrategyType == nil || *strategy.LegacyStrategyType != "round-robin" {
		t.Fatalf("expected model strategy to be mapped by model id, got ok=%v strategy=%+v", ok, strategy)
	}
	if len(strategy.FailureStatusCodes) != 2 || strategy.FailureStatusCodes[0] != 429 || strategy.FailureStatusCodes[1] != 500 {
		t.Fatalf("expected strategy status codes to survive snapshot assembly, got %+v", strategy.FailureStatusCodes)
	}

	targets := snapshot.AccessTargetsBySourceModelID[model.ID]
	if len(targets) != 1 {
		t.Fatalf("expected one access target, got %+v", targets)
	}
	target := targets[0]
	assertRuntimeIntPtr(t, target.TargetConnectionID, 901, "target connection")
	if target.TargetType != runtimeAccessTargetTypeConnection || target.Position != 2 || !target.IsEnabled {
		t.Fatalf("unexpected access target contract: %+v", target)
	}

	if target.TargetConnectionProfileID != profileID || target.TargetConnectionAPIFamily != "openai" {
		t.Fatalf("expected target connection provenance to stay profile/api-family scoped, got %+v", target)
	}
	if target.ConnectionEndpointFX == nil || target.ConnectionEndpointFX.ModelID != "router-openai" || target.ConnectionEndpointFX.EndpointID != 801 || target.ConnectionEndpointFX.FXRate != "1.25" {
		t.Fatalf("expected endpoint FX provenance on connection target, got %+v", target.ConnectionEndpointFX)
	}

	connection := snapshot.TerminalTargetsByID[901]
	if connection.ID != 901 || connection.ProfileID != profileID || connection.EndpointID != 801 || connection.Endpoint.ID != 801 {
		t.Fatalf("unexpected compiled terminal target identity: %+v", connection)
	}
	assertRuntimeStringPtr(t, connection.Name, "primary terminal", "connection name")
	assertRuntimeStringPtr(t, connection.OpenAITextCapability, providerauth.OpenAITextCapabilityChatCompletionsOnly, "OpenAI text capability")
	assertRuntimeIntPtr(t, connection.QPSLimit, 10, "qps limit")
	if connection.EncryptedEndpointAPIKey != "" || connection.UpstreamAuth == nil {
		t.Fatalf("expected compiled connection to move decrypted auth into upstream auth, got %+v", connection)
	}
	if connection.UpstreamAuth.AuthHeader != "Authorization" || connection.UpstreamAuth.AuthValue != "Bearer sk-runtime-contract" {
		t.Fatalf("unexpected upstream auth contract: %+v", connection.UpstreamAuth)
	}
	if _, ok := connection.UpstreamAuth.ControlledHeaderNames["authorization"]; !ok {
		t.Fatalf("expected authorization to be controlled, got %+v", connection.UpstreamAuth.ControlledHeaderNames)
	}
	if got, ok := connection.CustomHeaders["X-Custom"].(string); !ok || got != "allowed" {
		t.Fatalf("expected custom headers to be parsed, got %+v", connection.CustomHeaders)
	}

	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		t.Fatalf("compile routing plan: %v", err)
	}

	compiledModel := routingPlan.ModelsByID["router-openai"]
	if !compiledModel.HasStrategy || compiledModel.Strategy.ID != 303 || len(compiledModel.OrderedTerminalTargets) != 1 {
		t.Fatalf("expected routing plan to carry strategy and terminal target, got %+v", compiledModel)
	}
	if terminal := routingPlan.TerminalTargetsByID[901]; terminal.Endpoint.ID != 801 || terminal.UpstreamAuth == nil {
		t.Fatalf("expected compiled routing plan to index terminal targets by connection id, got %+v", terminal)
	}
}

type runtimePlanningSnapshotFakeTx struct {
	encryptedAPIKey string
}

func newRuntimePlanningSnapshotFakeTx(encryptedAPIKey string) *runtimePlanningSnapshotFakeTx {
	return &runtimePlanningSnapshotFakeTx{encryptedAPIKey: encryptedAPIKey}
}

func (tx *runtimePlanningSnapshotFakeTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {

	switch {
	case strings.Contains(query, "FROM model_configs") && strings.Contains(query, "model_configs.loadbalance_strategy_id"):
		return newRuntimePlanningRows([]any{11, 42, "openai", "router-openai", sql.NullInt32{Int32: 303, Valid: true}, sql.NullString{String: "dual_native", Valid: true}, time.Unix(2, 0).UTC(), true, false}), nil
	case strings.Contains(query, "FROM model_access_targets"):
		return newRuntimePlanningRows([]any{501, 42, 11, runtimeAccessTargetTypeConnection, sql.NullInt32{}, sql.NullString{}, sql.NullInt32{}, sql.NullString{}, sql.NullBool{}, sql.NullInt32{Int32: 901, Valid: true}, sql.NullInt32{Int32: 42, Valid: true}, sql.NullString{String: "openai", Valid: true}, 2, true, sql.NullString{String: "router-openai", Valid: true}, sql.NullInt32{Int32: 801, Valid: true}, sql.NullString{String: "1.25", Valid: true}}), nil
	case strings.Contains(query, "FROM loadbalance_strategies"):
		return newRuntimePlanningRows([]any{303, "contract round robin", "round-robin", []int32{429, 500}, "temporary", 25, 2.0, 0.1, 1000, 3, 5, 60}), nil
	case strings.Contains(query, "FROM connections") && strings.Contains(query, "JOIN endpoints"):
		return tx.connectionRows(), nil
	case strings.Contains(query, "FROM header_blocklist_rules"):
		return newRuntimePlanningRows([]any{"prefix", "x-blocked"}), nil
	default:

		return nil, fmt.Errorf("unexpected planning snapshot query: %s", query)
	}
}

func (tx *runtimePlanningSnapshotFakeTx) connectionRows() pgx.Rows {
	return newRuntimePlanningRows([]any{
		901, 42, "openai", 801, 2,
		sql.NullInt32{Int32: 10, Valid: true}, sql.NullInt32{Int32: 3, Valid: true}, sql.NullInt32{Int32: 4, Valid: true},
		sql.NullString{String: "primary terminal", Valid: true}, sql.NullString{String: "openai", Valid: true}, sql.NullString{String: `{"X-Custom":"allowed"}`, Valid: true}, sql.NullInt32{Int32: 701, Valid: true},
		sql.NullString{String: providerauth.OpenAITextCapabilityChatCompletionsOnly, Valid: true},
		sql.NullInt32{Int32: 701, Valid: true}, sql.NullString{String: runtimePricingUnitPerMillion, Valid: true}, sql.NullString{String: "USD", Valid: true},
		sql.NullString{String: "1", Valid: true}, sql.NullString{String: "2", Valid: true}, sql.NullString{String: "0.5", Valid: true}, sql.NullString{String: "0.25", Valid: true}, sql.NullString{String: "3", Valid: true}, sql.NullInt32{Int32: 3, Valid: true},

		801, sql.NullString{String: "primary endpoint", Valid: true}, "https://api.example.test/v1", tx.encryptedAPIKey,
	})
}

func (tx *runtimePlanningSnapshotFakeTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if strings.Contains(query, "FROM user_settings") {
		return runtimePlanningRow{values: []any{"EUR", "EUR"}}
	}
	return runtimePlanningRow{err: fmt.Errorf("unexpected planning snapshot query row: %s", query)}
}

func (tx *runtimePlanningSnapshotFakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}
func (tx *runtimePlanningSnapshotFakeTx) Commit(context.Context) error {
	return errors.New("not implemented")
}
func (tx *runtimePlanningSnapshotFakeTx) Rollback(context.Context) error {
	return errors.New("not implemented")
}
func (tx *runtimePlanningSnapshotFakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (tx *runtimePlanningSnapshotFakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (tx *runtimePlanningSnapshotFakeTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (tx *runtimePlanningSnapshotFakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}
func (tx *runtimePlanningSnapshotFakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not implemented")
}
func (tx *runtimePlanningSnapshotFakeTx) Conn() *pgx.Conn { return nil }

type runtimePlanningRows struct {
	values  [][]any
	current int
}

func newRuntimePlanningRows(values ...[]any) *runtimePlanningRows {
	return &runtimePlanningRows{values: values, current: -1}
}

func (rows *runtimePlanningRows) Close()                                       {}
func (rows *runtimePlanningRows) Err() error                                   { return nil }
func (rows *runtimePlanningRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *runtimePlanningRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *runtimePlanningRows) Next() bool {
	if rows.current+1 >= len(rows.values) {
		return false
	}
	rows.current++
	return true
}
func (rows *runtimePlanningRows) Scan(dest ...any) error {
	if rows.current < 0 || rows.current >= len(rows.values) {
		return errors.New("scan called without current row")
	}
	return assignRuntimePlanningValues(dest, rows.values[rows.current])
}
func (rows *runtimePlanningRows) Values() ([]any, error) { return rows.values[rows.current], nil }
func (rows *runtimePlanningRows) RawValues() [][]byte    { return nil }
func (rows *runtimePlanningRows) Conn() *pgx.Conn        { return nil }

type runtimePlanningRow struct {
	values []any
	err    error
}

func (row runtimePlanningRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return assignRuntimePlanningValues(dest, row.values)
}

func assignRuntimePlanningValues(dest []any, values []any) error {
	if len(dest) != len(values) {
		return fmt.Errorf("scan destination mismatch: got %d want %d", len(dest), len(values))
	}
	for index := range dest {
		if err := assignRuntimePlanningValue(dest[index], values[index]); err != nil {
			return fmt.Errorf("scan column %d: %w", index, err)
		}
	}
	return nil
}

func assignRuntimePlanningValue(destination any, value any) error {
	switch dest := destination.(type) {
	case *int:
		resolved, ok := value.(int)
		if !ok {
			return fmt.Errorf("expected int, got %T", value)
		}
		*dest = resolved
		return nil
	case *string:
		resolved, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		*dest = resolved
		return nil
	case *bool:

		resolved, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
		*dest = resolved
		return nil
	case *float64:
		resolved, ok := value.(float64)
		if !ok {
			return fmt.Errorf("expected float64, got %T", value)
		}
		*dest = resolved
		return nil
	case *time.Time:
		resolved, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("expected time.Time, got %T", value)
		}
		*dest = resolved
		return nil
	case *[]int32:
		resolved, ok := value.([]int32)
		if !ok {
			return fmt.Errorf("expected []int32, got %T", value)
		}
		*dest = resolved

		return nil
	case *sql.NullInt32:
		resolved, ok := value.(sql.NullInt32)
		if !ok {
			return fmt.Errorf("expected sql.NullInt32, got %T", value)
		}
		*dest = resolved
		return nil
	case *sql.NullString:
		resolved, ok := value.(sql.NullString)
		if !ok {
			return fmt.Errorf("expected sql.NullString, got %T", value)
		}
		*dest = resolved
		return nil
	case *sql.NullBool:
		resolved, ok := value.(sql.NullBool)
		if !ok {

			return fmt.Errorf("expected sql.NullBool, got %T", value)
		}
		*dest = resolved
		return nil
	case *sql.NullTime:
		resolved, ok := value.(sql.NullTime)
		if !ok {
			return fmt.Errorf("expected sql.NullTime, got %T", value)
		}
		*dest = resolved
		return nil
	case *sql.NullFloat64:
		resolved, ok := value.(sql.NullFloat64)
		if !ok {
			return fmt.Errorf("expected sql.NullFloat64, got %T", value)
		}
		*dest = resolved
		return nil
	default:
		return fmt.Errorf("unsupported scan destination %T", destination)
	}
}
