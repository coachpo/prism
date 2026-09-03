package integrationtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const c7SQLSHA256 = "d4afecb631dc70fa974d96cf0a877958ee3187c64675092260c6729b0fd396d0"

var c7DirectModels = []string{"codex/codex-auto-review", "codex/gpt-5.6-terra", "deepseek-v4-flash", "deepseek-v4-pro", "glm-5.3-flash", "codex/gpt-image-2", "codex/gpt-5.4-mini", "codex/gpt-5.5", "codex/gpt-5.6-luna", "gpt-5.6-luna", "muse-spark-1.2-contributor", "qwen3.8-flash"}
var c7InternalModels = []string{"DeepSeek-V4-Flash", "deepseek/deepseek-v4-flash-0731", "deepseek/deepseek-v4-pro", "z-ai/glm-5.3-flash"}
var c7Edges = []struct {
	parent, child string
	order         int
}{{"deepseek-v4-flash", "DeepSeek-V4-Flash", 1}, {"deepseek-v4-flash", "deepseek/deepseek-v4-flash-0731", 2}, {"deepseek-v4-pro", "deepseek/deepseek-v4-pro", 1}, {"glm-5.3-flash", "z-ai/glm-5.3-flash", 1}}

type c7Fixture struct {
	db   string
	conn *pgx.Conn
}
type c7Generations [5]int64

func c7Require(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatalf(format, args...)
	}
}

func TestDirectRequestEntryReclassificationPlan(t *testing.T) {
	ctx := t.Context()
	root := filepath.Clean("../../..")
	sqlBytes, err := os.ReadFile(filepath.Join(root, "artifacts/plans/direct-request-entry-reclassification.sql"))
	c7Require(t, err == nil, "read operator SQL: %v", err)
	harness := newPostgresHarness(t)

	t.Run("source contract and preview", func(t *testing.T) {
		assertC7SourceContract(t, root, sqlBytes)
		f := newC7Fixture(t, ctx, harness, true)
		before := c7FullSnapshot(t, ctx, f.conn)
		out, err := runC7Plan(ctx, harness, f.db, sqlBytes, false, nil)
		c7Require(t, err == nil, "preview: %v\n%s", err, out)
		c7Require(t, c7FullSnapshot(t, ctx, f.conn) == before, "preview changed persistent state")
		c7Require(t, strings.Contains(out, "will_update") && strings.Contains(out, "deepseek-v4-flash"), "preview omitted qualification or mapping evidence")
	})
	for _, tc := range []struct {
		name     string
		existing bool
		appends  int
	}{{"existing edges apply and noop", true, 0}, {"missing edges append and noop", false, 4}} {
		t.Run(tc.name, func(t *testing.T) { assertC7Apply(t, ctx, harness, sqlBytes, tc.existing, tc.appends) })
	}
	failures := []struct {
		name, mutation, want string
		existing             bool
		vars                 map[string]string
	}{
		{"backup token", "", "apply refused: pass -v backup_token=BACKUP_VERIFIED", true, map[string]string{"backup_token": "UNVERIFIED"}},
		{"quiesce token", "", "apply refused: stop Prism", true, map[string]string{"quiesce_token": "RUNNING"}},
		{"database token", "", "apply refused: -v expected_database", true, map[string]string{"expected_database": "wrong_database"}},
		{"migration history", "DELETE FROM prism_schema_migrations", "migration history does not contain", true, nil},
		{"default profile", "UPDATE profiles SET is_default=false WHERE id=1", "profile 1 is not the live Default profile", true, nil},
		{"inventory missing", "DELETE FROM model_configs WHERE model_id='qwen3.8-flash'", "must contain exactly 16", true, nil},
		{"inventory extra", "INSERT INTO model_configs (profile_id,api_family,model_id,openai_accepted_format,direct_request_enabled,is_enabled,created_at,updated_at) VALUES (1,'openai','unexpected-model','dual_native',true,true,now(),now())", "must contain exactly 16", true, nil},
		{"family mismatch", "UPDATE model_configs SET api_family='anthropic',openai_accepted_format=NULL,openai_image_operations=NULL WHERE model_id='DeepSeek-V4-Flash'", "profile/family/OpenAI dimensions", true, nil},
		{"text mode mismatch", "UPDATE model_configs SET openai_accepted_format='responses_only' WHERE model_id='DeepSeek-V4-Flash'", "profile/family/OpenAI dimensions", true, nil},
		{"image mode mismatch", "UPDATE model_configs SET openai_image_operations='generations' WHERE model_id='DeepSeek-V4-Flash'", "profile/family/OpenAI dimensions", true, nil},
		{"duplicate edge", "DROP INDEX uq_model_access_targets_source_target_model; INSERT INTO model_access_targets (profile_id,source_model_config_id,target_type,target_model_config_id,position,is_enabled,created_at,updated_at) SELECT 1,p.id,'model',c.id,3,true,now(),now() FROM model_configs p,model_configs c WHERE p.model_id='deepseek-v4-flash' AND c.model_id='DeepSeek-V4-Flash'", "required parent-to-child Model Target is duplicated", true, nil},
		{"disabled existing edge", "UPDATE model_access_targets x SET is_enabled=false FROM model_configs p,model_configs c WHERE x.source_model_config_id=p.id AND x.target_model_config_id=c.id AND p.model_id='deepseek-v4-flash' AND c.model_id='DeepSeek-V4-Flash'", "existing required edge is not an enabled Model Target", true, nil},
		{"position gap", "UPDATE model_access_targets x SET position=4 FROM model_configs c WHERE x.target_model_config_id=c.id AND c.model_id='deepseek/deepseek-v4-flash-0731'", "dense unique positions", true, nil},
		{"prospective cycle", "INSERT INTO model_access_targets (profile_id,source_model_config_id,target_type,target_model_config_id,position,is_enabled,created_at,updated_at) SELECT 1,c.id,'model',p.id,1,true,now(),now() FROM model_configs p,model_configs c WHERE p.model_id='deepseek-v4-flash' AND c.model_id='DeepSeek-V4-Flash'", "prospective Model Target graph contains a cycle", false, nil},
		{"flash model disabled", "UPDATE model_configs SET is_enabled=false WHERE model_id='DeepSeek-V4-Flash'", "DeepSeek identity conflict", true, nil},
		{"flash upstream mismatch", "UPDATE connections SET upstream_model_id='wrong-case' WHERE upstream_model_id='DeepSeek-V4-Flash'", "DeepSeek identity conflict", true, nil},
	}
	for _, tc := range failures {
		t.Run("fail closed "+tc.name, func(t *testing.T) {
			f := newC7Fixture(t, ctx, harness, tc.existing)
			if tc.mutation != "" {
				execUpstreamFixture(t, ctx, f.conn, "prepare "+tc.name, tc.mutation)
			}
			before := c7FullSnapshot(t, ctx, f.conn)
			out, err := runC7Plan(ctx, harness, f.db, sqlBytes, true, tc.vars)
			c7Require(t, err != nil && strings.Contains(out, tc.want), "expected %q failure, err=%v\n%s", tc.want, err, out)
			c7Require(t, c7FullSnapshot(t, ctx, f.conn) == before, "failed plan changed persistent state")
		})
	}
}

func assertC7SourceContract(t *testing.T, root string, sqlBytes []byte) {
	paths := []string{"artifacts/plans/direct-request-entry-reclassification.md", "artifacts/plans/direct-request-entry-reclassification.sql", "backend/tests/integration/direct_request_entry_reclassification_plan_test.go"}
	for _, path := range paths {
		c7Require(t, exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", path).Run() == nil, "C7 source is not Git-visible: %s", path)
	}
	runbook, err := os.ReadFile(filepath.Join(root, paths[0]))
	c7Require(t, err == nil, "read C7 runbook: %v", err)
	status, err := os.ReadFile(filepath.Join(root, "STATUS.md"))
	c7Require(t, err == nil, "read STATUS: %v", err)
	hash := fmt.Sprintf("%x", sha256.Sum256(sqlBytes))
	c7Require(t, hash == c7SQLSHA256 && bytes.Contains(runbook, []byte("SQL SHA-256: "+hash)), "reviewed SQL/runbook hash binding changed: %s", hash)
	c7Require(t, bytes.Contains(runbook, []byte(paths[1])) && bytes.Contains(runbook, []byte(paths[2])), "runbook omits canonical SQL or acceptance-test path")
	c7Require(t, !bytes.Contains(status, []byte("Git-ignored local bundle")) && !bytes.Contains(status, []byte("not shipped in release source")), "STATUS still describes C7 as ignored or unshipped")
	for _, id := range append(append([]string{}, c7DirectModels...), c7InternalModels...) {
		c7Require(t, bytes.Contains(sqlBytes, []byte(id)) && bytes.Contains(runbook, []byte(id)), "C7 identity omitted: %s", id)
	}
	for _, edge := range c7Edges {
		c7Require(t, bytes.Contains(sqlBytes, []byte(fmt.Sprintf("('%s', '%s', %d)", edge.parent, edge.child, edge.order))) && bytes.Contains(runbook, []byte(edge.parent+" --Model Target--> "+edge.child)), "C7 edge omitted: %s -> %s", edge.parent, edge.child)
	}
	for _, token := range []string{"APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1", "BACKUP_VERIFIED", "PRISM_STOPPED", "BEGIN ISOLATION LEVEL SERIALIZABLE READ WRITE", "BEGIN ISOLATION LEVEL SERIALIZABLE READ ONLY"} {
		c7Require(t, bytes.Contains(sqlBytes, []byte(token)), "C7 safety token omitted: %s", token)
	}
}

func newC7Fixture(t *testing.T, ctx context.Context, h postgresHarness, existingEdges bool) c7Fixture {
	db := "c7_reclass_" + randomSuffix(t)
	conn := h.openDatabase(t, ctx, db)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	profileID := seedMigrationProfile(t, ctx, conn, "c7", now)
	c7Require(t, profileID == 1, "Default profile id=%d, want 1", profileID)
	execUpstreamFixture(t, ctx, conn, "mark Default profile", "UPDATE profiles SET is_default=true WHERE id=1")
	models := append(append([]string{}, c7DirectModels...), c7InternalModels...)
	ids := map[string]int{}
	for _, model := range models {
		ids[model] = seedModelForUpstreamHistory(t, ctx, conn, 1, model)
	}
	execUpstreamFixture(t, ctx, conn, "preserve disabled whitelist rows", "UPDATE model_configs SET is_enabled=false WHERE model_id=ANY($1)", []string{"codex/gpt-image-2", "gpt-5.6-luna"})
	endpointID := seedEndpointForUpstreamHistory(t, ctx, conn, 1, "c7-endpoint")
	owners := [][2]string{{"deepseek-v4-flash", "deepseek-v4-flash"}, {"DeepSeek-V4-Flash", "DeepSeek-V4-Flash"}, {"deepseek/deepseek-v4-flash-0731", "deepseek/deepseek-v4-flash-0731"}, {"deepseek-v4-pro", "deepseek-v4-pro"}, {"glm-5.3-flash", "glm-5.3-flash"}}
	firstConnection := 0
	for i, owner := range owners {
		connectionID := seedConnectionForUpstreamHistory(t, ctx, conn, 1, endpointID, now)
		execUpstreamFixture(t, ctx, conn, "set upstream id", "UPDATE connections SET upstream_model_id=$1 WHERE id=$2", owner[1], connectionID)
		seedOwnerForUpstreamHistory(t, ctx, conn, 1, ids[owner[0]], connectionID, 0, now)
		if i == 0 {
			firstConnection = connectionID
		}
	}
	if existingEdges {
		for _, edge := range c7Edges {
			execUpstreamFixture(t, ctx, conn, "seed model edge", "INSERT INTO model_access_targets (profile_id,source_model_config_id,target_type,target_model_config_id,position,is_enabled,created_at,updated_at) VALUES (1,$1,'model',$2,$3,true,$4,$4)", ids[edge.parent], ids[edge.child], edge.order, now)
		}
	}
	ensureDailyLogPartition(t, ctx, conn, "request_logs", now, "c7")
	ensureDailyLogPartition(t, ctx, conn, "usage_request_events", now, "c7")
	seedRowsForUpstreamHistory(t, ctx, conn, 1, endpointID, firstConnection, now)
	execUpstreamFixture(t, ctx, conn, "seed catalog binding", "INSERT INTO model_catalog_bindings (model_config_id,provider_id,catalog_model_id,match_source,catalog_revision,fetched_at,source_name,updated_at) VALUES ($1,'openai','deepseek-v4-flash','manual','c7-models',$2,'C7',$2)", ids["deepseek-v4-flash"], now)
	execUpstreamFixture(t, ctx, conn, "seed pi binding", "INSERT INTO model_pi_catalog_bindings (model_config_id,provider_id,catalog_model_id,api,bind_source,catalog_revision,fetched_at,source_name,prism_model_id_at_bind,updated_at) VALUES ($1,'openai','deepseek-v4-flash','openai-responses','manual','c7-pi',$2,'C7','deepseek-v4-flash',$2)", ids["deepseek-v4-flash"], now)
	execUpstreamFixture(t, ctx, conn, "seed pricing sentinel", "INSERT INTO pricing_templates (profile_id,name,description,current_revision_id,created_at,updated_at,deleted_at) VALUES (1,'c7-pricing',NULL,NULL,$1,$1,NULL)", now)
	execUpstreamFixture(t, ctx, conn, "seed global generations", "UPDATE runtime_cache_generations SET version=7,updated_at=$1,reason='c7-fixture'", now)
	execUpstreamFixture(t, ctx, conn, "seed profile generations", "INSERT INTO runtime_cache_generations (domain,scope_type,scope_id,version,updated_at,reason) VALUES ('profile_runtime','profile','1',13,$1,'c7-fixture'),('runtime_planning','profile','1',17,$1,'c7-fixture')", now)
	execUpstreamFixture(t, ctx, conn, "seed witness generation", "INSERT INTO route_witness_generations (profile_id,generation,updated_at) VALUES (1,23,$1)", now)
	return c7Fixture{db: db, conn: conn}
}

func runC7Plan(ctx context.Context, h postgresHarness, db string, sqlBytes []byte, apply bool, overrides map[string]string) (string, error) {
	vars := map[string]string{}
	if apply {
		vars = map[string]string{"apply_token": "APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1", "backup_token": "BACKUP_VERIFIED", "quiesce_token": "PRISM_STOPPED", "expected_database": db}
	}
	for key, value := range overrides {
		vars[key] = value
	}
	binary := "psql"
	args := []string{"--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--dbname", h.connectionString(db)}
	if container := h.dumpContainerName(); container != "" {
		binary = "docker"
		args = []string{"exec", "--interactive", container, "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--username", "prism", "--dbname", db}
	}
	for key, value := range vars {
		args = append(args, "--set", key+"="+value)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = bytes.NewReader(sqlBytes)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertC7Apply(t *testing.T, ctx context.Context, h postgresHarness, sqlBytes []byte, existing bool, wantAppends int) {
	f := newC7Fixture(t, ctx, h, existing)
	beforePreserved := c7PreservedSnapshot(t, ctx, f.conn)
	beforeTargets := c7Value[int](t, ctx, f.conn, "SELECT count(*) FROM model_access_targets")
	targetMax := c7Value[int](t, ctx, f.conn, "SELECT coalesce(max(id),0) FROM model_access_targets")
	existingTargets := c7TargetsThrough(t, ctx, f.conn, targetMax)
	beforeGen := c7GenerationState(t, ctx, f.conn)
	out, err := runC7Plan(ctx, h, f.db, sqlBytes, true, nil)
	c7Require(t, err == nil, "apply: %v\n%s", err, out)
	c7Require(t, strings.Contains(out, "model_qualification") && (wantAppends > 0) == strings.Contains(out, "model_target_append"), "unexpected change summary:\n%s", out)
	c7Require(t, c7Value[int](t, ctx, f.conn, "SELECT count(*) FROM model_configs WHERE direct_request_enabled IS DISTINCT FROM (model_id=ANY($1))", c7DirectModels) == 0, "direct-entry postcondition failed")
	wantEdges := "deepseek-v4-flash>DeepSeek-V4-Flash@1:true,deepseek-v4-flash>deepseek/deepseek-v4-flash-0731@2:true,deepseek-v4-pro>deepseek/deepseek-v4-pro@1:true,glm-5.3-flash>z-ai/glm-5.3-flash@1:true"
	edgeQuery := "SELECT coalesce(string_agg(p.model_id||'>'||c.model_id||'@'||x.position||':'||x.is_enabled,',' ORDER BY p.id,x.position),'') FROM model_access_targets x JOIN model_configs p ON p.id=x.source_model_config_id JOIN model_configs c ON c.id=x.target_model_config_id WHERE x.target_type='model'"
	c7Require(t, c7Value[string](t, ctx, f.conn, edgeQuery) == wantEdges, "parent-to-child edge postcondition failed")
	c7Require(t, c7PreservedSnapshot(t, ctx, f.conn) == beforePreserved, "apply changed preserved business rows")
	c7Require(t, c7TargetsThrough(t, ctx, f.conn, targetMax) == existingTargets, "apply changed a pre-existing access target")
	c7Require(t, c7Value[int](t, ctx, f.conn, "SELECT count(*) FROM model_access_targets") == beforeTargets+wantAppends, "unexpected target row count")
	afterGen := c7GenerationState(t, ctx, f.conn)
	for i := range beforeGen {
		c7Require(t, afterGen[i] == beforeGen[i]+1, "generation[%d]=%d, want %d", i, afterGen[i], beforeGen[i]+1)
	}
	beforeNoop := c7FullSnapshot(t, ctx, f.conn)
	out, err = runC7Plan(ctx, h, f.db, sqlBytes, true, nil)
	c7Require(t, err == nil, "second apply: %v\n%s", err, out)
	c7Require(t, c7FullSnapshot(t, ctx, f.conn) == beforeNoop, "second apply was not a true no-op")
	c7Require(t, !strings.Contains(out, "model_qualification") && !strings.Contains(out, "model_target_append"), "second apply reported changes:\n%s", out)
}

func c7FullSnapshot(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	return c7Value[string](t, ctx, conn, "SELECT jsonb_build_object('profiles',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM profiles x),'migrations',(SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM prism_schema_migrations x),'models',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM model_configs x),'targets',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM model_access_targets x),'connections',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM connections x),'endpoints',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM endpoints x),'requests',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM request_logs x),'usage',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM usage_request_events x),'catalog',(SELECT jsonb_agg(to_jsonb(x) ORDER BY model_config_id) FROM model_catalog_bindings x),'pi',(SELECT jsonb_agg(to_jsonb(x) ORDER BY model_config_id) FROM model_pi_catalog_bindings x),'pricing',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM pricing_templates x),'runtime',(SELECT jsonb_agg(to_jsonb(x) ORDER BY domain,scope_type,scope_id) FROM runtime_cache_generations x),'witness',(SELECT jsonb_agg(to_jsonb(x) ORDER BY profile_id) FROM route_witness_generations x),'sequence',(SELECT jsonb_build_object('last_value',last_value,'is_called',is_called) FROM model_access_targets_id_seq))::text")
}
func c7PreservedSnapshot(t *testing.T, ctx context.Context, conn *pgx.Conn) string {
	return c7Value[string](t, ctx, conn, "SELECT jsonb_build_object('profiles',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM profiles x),'migrations',(SELECT jsonb_agg(to_jsonb(x) ORDER BY version) FROM prism_schema_migrations x),'models',(SELECT jsonb_agg(to_jsonb(x)-'direct_request_enabled' ORDER BY id) FROM model_configs x),'connections',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM connections x),'endpoints',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM endpoints x),'requests',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM request_logs x),'usage',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM usage_request_events x),'catalog',(SELECT jsonb_agg(to_jsonb(x) ORDER BY model_config_id) FROM model_catalog_bindings x),'pi',(SELECT jsonb_agg(to_jsonb(x) ORDER BY model_config_id) FROM model_pi_catalog_bindings x),'pricing',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM pricing_templates x),'other_generations',(SELECT jsonb_agg(to_jsonb(x) ORDER BY domain,scope_type,scope_id) FROM runtime_cache_generations x WHERE NOT (domain IN ('profile_runtime','runtime_planning') AND (scope_type,scope_id) IN (('global','*'),('profile','1')))))::text")
}
func c7Value[T any](t *testing.T, ctx context.Context, conn *pgx.Conn, query string, args ...any) T {
	t.Helper()
	var value T
	if err := conn.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func c7TargetsThrough(t *testing.T, ctx context.Context, conn *pgx.Conn, maxID int) string {
	return c7Value[string](t, ctx, conn, "SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]')::text FROM model_access_targets x WHERE id<=$1", maxID)
}
func c7GenerationState(t *testing.T, ctx context.Context, conn *pgx.Conn) c7Generations {
	t.Helper()
	var v c7Generations
	err := conn.QueryRow(ctx, "SELECT (SELECT version FROM runtime_cache_generations WHERE domain='profile_runtime' AND scope_type='global' AND scope_id='*'),(SELECT version FROM runtime_cache_generations WHERE domain='runtime_planning' AND scope_type='global' AND scope_id='*'),(SELECT version FROM runtime_cache_generations WHERE domain='profile_runtime' AND scope_type='profile' AND scope_id='1'),(SELECT version FROM runtime_cache_generations WHERE domain='runtime_planning' AND scope_type='profile' AND scope_id='1'),(SELECT generation FROM route_witness_generations WHERE profile_id=1)").Scan(&v[0], &v[1], &v[2], &v[3], &v[4])
	c7Require(t, err == nil, "load generation state: %v", err)
	return v
}
