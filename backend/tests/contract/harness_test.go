package contracttest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sharedPostgresHarness = harness
	if err := prepareContractTemplateDatabase(harness); err != nil {
		fmt.Fprintln(os.Stderr, err)
		cleanupSharedPostgresHarness(harness)
		os.Exit(1)
	}
	code := m.Run()
	cleanupSharedPostgresHarness(harness)
	os.Exit(code)
}

func cleanupSharedPostgresHarness(harness testPostgresHarness) {
	if harness.containerName == "" {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", harness.containerName).Run()
}
