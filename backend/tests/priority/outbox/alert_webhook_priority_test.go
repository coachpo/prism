package outbox_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlertWebhookOutboxRetryAndScheduling(t *testing.T) {
	backendRoot := filepath.Clean(filepath.Join("..", "..", ".."))

	assertFileContains(t, filepath.Join(backendRoot, "internal/platform/lifecycle/production.go"), []string{
		"alerting.NewStore",
		"alertWebhookOutbox.RegisterBackgroundWorker",
		"backgroundJobsPool",
	})
	assertFileContains(t, filepath.Join(backendRoot, "internal/httpapi/runtime/feedback_pipeline.go"), []string{
		"alertOutbox.EnqueueTx",
		"banned",
		"unbanned",
		"recovered",
	})
	assertFileNotContains(t, filepath.Join(backendRoot, "internal/httpapi/runtime/feedback_pipeline.go"), []string{
		"http.Post(",
		"http.DefaultClient.Do(",
	})
	assertFileContains(t, filepath.Join(backendRoot, "internal/platform/alerting/outbox.go"), []string{
		"alert_webhook_worker",
		"FOR UPDATE SKIP LOCKED",
		"alert_webhook_outbox",
		"backoffForAttempt",
		"RegisterBackgroundWorker",
		"handleScheduledPost",
	})
	assertFileContains(t, filepath.Join(backendRoot, "migrations/000011_alert_webhook_outbox.sql"), []string{
		"alert_webhook_outbox",
		"idx_alert_webhook_outbox_idempotency_key",
		"idx_alert_webhook_outbox_due",
		"idx_alert_webhook_outbox_stale_locks",
		"idx_alert_webhook_outbox_dead_letters",
	})
}

func assertFileContains(t *testing.T, filePath string, markers []string) {
	t.Helper()
	content := readFileString(t, filePath)
	for _, marker := range markers {
		if !strings.Contains(content, marker) {
			t.Fatalf("expected %s to contain %q", filePath, marker)
		}
	}
}

func assertFileNotContains(t *testing.T, filePath string, markers []string) {
	t.Helper()
	content := readFileString(t, filePath)
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			t.Fatalf("expected %s to not contain %q", filePath, marker)
		}
	}
}

func readFileString(t *testing.T, filePath string) string {
	t.Helper()
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read %s: %v", filePath, err)
	}
	return string(content)
}
