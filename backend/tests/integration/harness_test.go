package integrationtest

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := prepareTemplateDatabase(harness); err != nil {
		fmt.Fprintln(os.Stderr, err)
		cleanupSharedPostgresHarness(harness)
		os.Exit(1)
	}
	sharedIntegrationPostgresHarness = harness

	code := m.Run()
	cleanupSharedPostgresHarness(harness)
	os.Exit(code)
}
