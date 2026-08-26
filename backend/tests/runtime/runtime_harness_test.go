package runtimetest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/tests/testsupport/containername"
)

func TestMain(m *testing.M) {
	harness, err := startSharedPostgresHarness()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sharedPostgresHarness = harness
	code := m.Run()
	cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if harness.containerName != "" {
		_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", harness.containerName).Run()
	}
	os.Exit(code)
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	// An externally provisioned PostgreSQL lets the suite run without shelling
	// out to Docker at all, which is what closed-loop verification needs to be
	// able to certify that the case left no stray processes behind. It also lets
	// CI attach a service container instead of running docker-in-docker. The
	// credentials stay the harness defaults (prism/prism).
	if externalPort := strings.TrimSpace(os.Getenv("PRISM_TEST_POSTGRES_PORT")); externalPort != "" {
		if err := waitForPostgres(externalPort); err != nil {
			return testPostgresHarness{}, err
		}
		return testPostgresHarness{hostPort: externalPort}, nil
	}
	containerName := containername.Prefix() + "-s14-runtime-" + randomSuffix()
	if err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "--tmpfs", "/var/lib/postgresql/data:rw", "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return testPostgresHarness{}, err
	}
	hostPort, err := dockerPort(containerName)
	if err != nil {
		return testPostgresHarness{}, err
	}
	if err := waitForPostgres(hostPort); err != nil {
		return testPostgresHarness{}, err
	}
	return testPostgresHarness{containerName: containerName, hostPort: hostPort}, nil
}
