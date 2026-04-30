package static

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type checkerOutput struct {
	Violations []struct {
		Code   string `json:"code"`
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"violations"`
}

func TestStaticNegativeFixturesRejectBypassClasses(t *testing.T) {
	tests := []struct {
		name       string
		check      string
		files      map[string]string
		wantReason string
	}{
		{
			name:  "direct DB",
			check: "direct-db",
			files: mergeFiles(baseServerLaneFiles(), map[string]string{
				"internal/example/direct_db.go": `package example

import "github.com/jackc/pgx/v5/pgxpool"

func bypass() { _, _ = pgxpool.New(nil, "") }
`,
			}),
			wantReason: "direct pgxpool construction outside internal/platform/db",
		},
		{
			name:  "unmanaged goroutine",
			check: "unmanaged-goroutine",
			files: map[string]string{
				"internal/example/worker.go": `package example

func bypass() { go func() {}() }
`,
			},
			wantReason: "recurring or delayed background work must be owned by internal/platform/background",
		},
		{
			name:       "unregistered background work",
			check:      "unregistered-background-work",
			files:      unregisteredBackgroundFixture(),
			wantReason: "missing registered background worker evidence handleScheduledRefresh",
		},
		{
			name:       "direct telemetry export",
			check:      "direct-telemetry-export",
			files:      directTelemetryFixture(),
			wantReason: "runtime finalizer directly persists telemetry",
		},
		{
			name:       "management side effects",
			check:      "management-sideeffects",
			files:      managementSideEffectsFixture(),
			wantReason: "dashboard invalidation remains inline after transaction",
		},
		{
			name:       "direct email",
			check:      "direct-email",
			files:      directEmailFixture(),
			wantReason: "auth request path contains direct email send evidence",
		},
		{
			name:       "direct cache mutation",
			check:      "direct-cache-mutation",
			files:      directCacheFixture(),
			wantReason: "runtime cache middleware remains authoritative or fallback-driven",
		},
		{
			name:       "unclassified side effect",
			check:      "unclassified-side-effect",
			files:      unclassifiedSideEffectFixture(),
			wantReason: "observe-only or default priority bypass remains",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, test.files)
			output, err := runChecker(t, root, test.check)
			if err == nil {
				t.Fatalf("expected %s fixture to fail, got success:\n%s", test.check, output)
			}
			parsed := parseCheckerOutput(t, output)
			if len(parsed.Violations) == 0 {
				t.Fatalf("expected JSON violations for %s, got:\n%s", test.check, output)
			}
			for _, violation := range parsed.Violations {
				if violation.Code == test.check && strings.Contains(violation.Reason, test.wantReason) {
					return
				}
			}
			t.Fatalf("expected %s violation containing %q, got %+v", test.check, test.wantReason, parsed.Violations)
		})
	}
}

func runChecker(t *testing.T, root string, check string) ([]byte, error) {
	t.Helper()
	command := exec.Command("go", "run", "./cmd/prism-priority-check", "--root", root, "--check="+check, "--format=json", "./...")
	command.Dir = backendRoot(t)
	return command.Output()
}

func parseCheckerOutput(t *testing.T, output []byte) checkerOutput {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(output))
	var parsed checkerOutput
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatalf("decode checker JSON: %v\n%s", err, output)
	}
	return parsed
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir fixture path %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	return root
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func mergeFiles(groups ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

func baseServerLaneFiles() map[string]string {
	return map[string]string{
		"internal/platform/http/server.go": strings.Join([]string{
			"package platformhttp",
			"const evidence = `databasePools.Management.Raw() databasePools.RuntimeExecution.Raw() databasePools.RuntimeTelemetry.Raw() databasePools.RuntimeFeedback.Raw() databasePools.Realtime.Raw() databasePools.CacheRefresh.Raw() databasePools.BackgroundJobs.Raw() FeedbackPool: runtimeFeedbackPool RealtimePool: realtimePool RefreshPool: cacheRefreshPool ProxyKeyUsagePool: backgroundJobsPool`",
		}, "\n"),
	}
}

func unregisteredBackgroundFixture() map[string]string {
	files := mergeFiles(baseServerLaneFiles(), map[string]string{
		"internal/platform/http/server.go": `package platformhttp

const evidence = ` + "`" + `background.NewScheduler(background.Config{}) runtimePlanningCache.RegisterBackgroundWorker managementAuthService.RegisterBackgroundWorkers asyncDashboardPublisher.RegisterBackgroundWorker runtimeService.RegisterBackgroundWorkers emailOutbox.RegisterBackgroundWorker backgroundScheduler.Start(context.Background()) backgroundScheduler.Stop(ctx, time.Now().Add(5*time.Second))` + "`" + `
`,
	})
	workerFiles := map[string][]string{
		"internal/httpapi/runtime/cache.go":                          {"RegisterBackgroundWorker", "runtime_shared_cache_refresh"},
		"internal/httpapi/runtime/telemetry_outbox.go":               {"RegisterBackgroundWorker", "runtime_telemetry_outbox", "handleScheduledTelemetry"},
		"internal/httpapi/runtime/runtime_side_effects.go":           {"RegisterBackgroundWorker", "runtime_side_effects_activity", "handleRuntimeActivity"},
		"internal/httpapi/runtime/feedback_pipeline.go":              {"RegisterBackgroundWorker", "runtime_feedback_pipeline", "handleScheduledFeedback"},
		"internal/httpapi/realtime/async_publisher.go":               {"RegisterBackgroundWorker", "async_dashboard_publisher", "handleScheduledPublish"},
		"internal/httpapi/management/auth/proxy_key_usage_writer.go": {"RegisterBackgroundWorker", "proxy_key_usage_writer", "handleScheduledFlush"},
		"internal/platform/managementsideeffects/outbox.go":          {"RegisterBackgroundWorker", "management_side_effect_outbox", "handleScheduledDispatch"},
		"internal/platform/email/outbox/outbox.go":                   {"RegisterBackgroundWorker", "email_outbox_worker", "handleScheduledSend", "FOR UPDATE SKIP LOCKED"},
	}
	for path, markers := range workerFiles {
		files[path] = "package fixture\nconst evidence = `" + strings.Join(markers, " ") + "`\n"
	}
	return files
}

func directTelemetryFixture() map[string]string {
	return map[string]string{
		"internal/httpapi/runtime/observability.go":        "package runtime\nconst evidence = `RuntimeActivityIntent SubmitRuntimeActivity RuntimeSideEffectAccepted telemetryOutbox.Enqueue`\n",
		"internal/httpapi/runtime/runtime_side_effects.go": "package runtime\nconst evidence = `runtime_side_effects_activity RegisterBackgroundWorker handleRuntimeActivity outbox.Enqueue`\n",
		"internal/httpapi/runtime/runtime.go":              "package runtime\n",
		"internal/httpapi/runtime/feedback_pipeline.go":    "package runtime\nconst evidence = `runtime_feedback TryEnqueue runtime_feedback_pipeline handleScheduledFeedback`\n",
	}
}

func managementSideEffectsFixture() map[string]string {
	return map[string]string{
		"internal/platform/http/server.go":                  "package platformhttp\nconst evidence = `managementsideeffects.NewDispatcher managementSideEffects.RegisterBackgroundWorker SideEffects: managementSideEffects`\n",
		"internal/platform/managementsideeffects/outbox.go": "package managementsideeffects\nconst evidence = `management_side_effect_outbox EventDashboardSnapshotInvalidate FOR UPDATE SKIP LOCKED failed_permanent defaultBatchSize       = 50 defaultConcurrency     = 4 defaultRetryCap        = 8 background.PriorityNormalBackground`\n",
		"internal/httpapi/management/stats/service.go":      "package stats\nconst evidence = `s.invalidateDashboardAggregateSnapshot(profileID) enqueueDashboardInvalidation managementsideeffects.InsertTx managementsideeffects.AfterCommit EventDashboardSnapshotInvalidate`\n",
		"migrations/000007_management_outbox.sql":           "management_outbox idx_management_outbox_dedupe_key idx_management_outbox_polling failed_permanent",
	}
}

func directEmailFixture() map[string]string {
	return map[string]string{
		"internal/platform/http/server.go":           "package platformhttp\nconst evidence = `outbox.NewStore EmailOutbox: emailOutbox emailOutbox.RegisterBackgroundWorker backgroundJobsPool`\n",
		"internal/httpapi/management/auth/routes.go": "package auth\nconst evidence = `SendPasswordResetEmail( enqueueAuthEmail outbox.KindEmailVerificationOTP outbox.KindPasswordReset`\n",
		"internal/platform/email/outbox/outbox.go":   "package outbox\nconst evidence = `email_outbox_worker FOR UPDATE SKIP LOCKED email_secret_ciphertext backoffForAttempt sanitizeError RegisterBackgroundWorker`\n",
		"migrations/000008_email_outbox.sql":         "email_outbox idx_email_outbox_idempotency_key idx_email_outbox_due idx_email_outbox_stale_locks idx_email_outbox_dead_letters",
	}
}

func directCacheFixture() map[string]string {
	return map[string]string{
		"internal/platform/http/runtime_cache_invalidation.go": "package platformhttp\nconst evidence = `RuntimeGenerationBumpsForRefresh WithBeforeCommitHook AdvanceRuntimeCacheGenerations ScheduleRefresh(request) RefreshNow(ctx, request)`\n",
		"internal/httpapi/management/auth/service.go":          "package auth\n",
		"internal/httpapi/runtime/generations.go":              "package runtime\nconst evidence = `runtime_cache_generations ON CONFLICT version = runtime_cache_generations.version + 1`\n",
		"internal/httpapi/runtime/cache.go":                    "package runtime\nconst evidence = `ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes()) ErrRuntimeSnapshotGenerationChanged LoadFreshRuntimeAuthSettings LoadFreshRuntimeProxyKeyRecord runtime_shared_cache_refresh`\n",
	}
}

func unclassifiedSideEffectFixture() map[string]string {
	return map[string]string{
		"internal/platform/http/admission.go": "package platformhttp\nconst evidence = `managementRouteSpecs admission.NewController priority.PriorityManagement priority.PriorityProxy observe-only`\n",
	}
}
