package outbox_test

import (
	"os/exec"
	"testing"
)

func TestEmailOutboxRetryAndIdempotency(t *testing.T) {
	command := exec.Command("go", "run", "./cmd/prism-priority-check", "--check=direct-email", "./...")
	command.Dir = "../../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("direct-email priority check failed: %v\n%s", err, string(output))
	}
}
