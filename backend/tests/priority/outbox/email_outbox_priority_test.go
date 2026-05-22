package outbox_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmailOutboxRetryAndIdempotency(t *testing.T) {
	backendRoot := filepath.Clean(filepath.Join("..", "..", ".."))

	assertFileContains(t, filepath.Join(backendRoot, "internal/platform/lifecycle/production.go"), []string{
		"outbox.NewStore",
		"EmailOutbox: emailOutbox",
		"emailOutbox.RegisterBackgroundWorker",
		"backgroundJobsPool",
	})

	assertFileContains(t, filepath.Join(backendRoot, "internal/httpapi/management/auth/routes.go"), []string{
		"enqueueAuthEmail",
		"s.emailOutbox.EnqueueTx",
		"outbox.KindEmailVerificationOTP",
		"outbox.KindPasswordReset",
	})
	assertFileNotContains(t, filepath.Join(backendRoot, "internal/httpapi/management/auth/routes.go"), []string{
		"SendEmailVerificationOTP(",
		"SendPasswordResetEmail(",
		"smtp.",
		"NewSMTPMailer",
	})

	assertFileContains(t, filepath.Join(backendRoot, "internal/platform/email/outbox/outbox.go"), []string{
		"email_outbox_worker",
		"FOR UPDATE SKIP LOCKED",
		"email_secret_ciphertext",
		"backoffForAttempt",
		"sanitizeError",
		"RegisterBackgroundWorker",
		"handleScheduledSend",
	})

	assertFileContains(t, filepath.Join(backendRoot, "migrations/000001_initial_schema.sql"), []string{
		"email_outbox",
		"idx_email_outbox_idempotency_key",
		"idx_email_outbox_due",
		"idx_email_outbox_stale_locks",
		"idx_email_outbox_dead_letters",
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
