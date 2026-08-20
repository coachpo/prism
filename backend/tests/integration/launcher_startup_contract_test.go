package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/tests/testsupport/containername"
)

const (
	launcherBackendPort  = 8000
	launcherFrontendPort = 5173
	launcherDatabasePort = 15432
	launcherDatabaseURL  = "postgres://prism:prism@localhost:15432/prism?sslmode=disable"
)

func TestStartShHeadlessSeedsMissingBootstrap(t *testing.T) {
	preflightStartShLauncher(t, false)

	configPath := filepath.Join(t.TempDir(), "config.json")
	run := startShLauncher(t, "headless", configPath, 120*time.Second)

	waitForStartShReadiness(t, run, false)
	assertLauncherOutputLine(t, run, fmt.Sprintf("Backend:  http://localhost:%d", launcherBackendPort))
	assertLauncherOutputLine(t, run, "Config:   "+configPath)
	assertLauncherBootstrapConfig(t, configPath)
}

func TestStartShHeadlessPreservesExistingBootstrap(t *testing.T) {
	preflightStartShLauncher(t, false)

	configPath := filepath.Join(t.TempDir(), "config.json")
	runBackendPrintEffectiveStartupSettings(t, configPath, launcherDatabaseURL)
	before := setStartupBootstrapFileModTime(t, configPath, time.Date(2026, 5, 25, 17, 0, 0, 0, time.UTC))

	run := startShLauncher(t, "headless", configPath, 120*time.Second)

	waitForStartShReadiness(t, run, false)
	assertLauncherOutputLine(t, run, fmt.Sprintf("Backend:  http://localhost:%d", launcherBackendPort))
	assertLauncherOutputLine(t, run, "Config:   "+configPath)
	assertStartupBootstrapFileStatePreserved(t, configPath, before)
}

func TestStartShFullUsesUpdatedProxyTarget(t *testing.T) {
	if os.Getenv("PRISM_RUN_LAUNCHER_FULL_TEST") != "1" {
		t.Skipf("set PRISM_RUN_LAUNCHER_FULL_TEST=1 to run full start.sh launcher coverage")
	}
	preflightStartShLauncher(t, true)

	configPath := filepath.Join(t.TempDir(), "config.json")
	run := startShLauncher(t, "full", configPath, 180*time.Second)

	waitForStartShReadiness(t, run, true)
	assertLauncherOutputLine(t, run, fmt.Sprintf("Backend:  http://localhost:%d", launcherBackendPort))
	assertLauncherOutputLine(t, run, "Config:   "+configPath)
	assertLauncherOutputLine(t, run, fmt.Sprintf("Frontend: http://localhost:%d", launcherFrontendPort))
	assertLauncherBootstrapConfig(t, configPath)
}

type startShLauncherRun struct {
	ctx            context.Context
	cancel         context.CancelFunc
	command        *exec.Cmd
	output         *lockedLauncherOutput
	done           chan struct{}
	waitErr        error
	waitErrMu      sync.Mutex
	repoRoot       string
	composeProject string
	configPath     string
	launcherLock   *launcherFileLock
}

type launcherFileLock struct {
	file *os.File
	path string
}

type lockedLauncherOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (o *lockedLauncherOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(data)
}

func (o *lockedLauncherOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.String()
}

func preflightStartShLauncher(t *testing.T, fullMode bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skipf("start.sh launcher tests require Unix process-group cleanup, got runtime.GOOS=%s", runtime.GOOS)
	}

	repoRoot := launcherRepoRoot(t)
	startShPath := filepath.Join(repoRoot, "start.sh")
	info, err := os.Stat(startShPath)
	if err != nil {
		t.Skipf("start.sh launcher preflight cannot stat %s: %v", startShPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Skipf("start.sh launcher preflight requires executable %s", startShPath)
	}

	preflightLauncherCommand(t, "bash")
	preflightLauncherCommand(t, "go")
	preflightLauncherCommand(t, "docker")
	if fullMode {
		preflightLauncherCommand(t, "pnpm")
	}
	preflightDockerDaemon(t)
	preflightDockerCompose(t)
	// headless starts no Vite and must not touch the frontend port, so it does
	// not require one to be free either.
	ports := []int{launcherBackendPort, launcherDatabasePort}
	if fullMode {
		ports = append(ports, launcherFrontendPort)
	}
	for _, port := range ports {
		preflightLauncherPort(t, port)
	}
}

func preflightLauncherCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("start.sh launcher preflight missing %q: %v", name, err)
	}
}

func preflightDockerDaemon(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Skipf("start.sh launcher preflight timed out checking docker daemon: %v", ctx.Err())
	}
	if err != nil {
		t.Skipf("start.sh launcher preflight cannot reach docker daemon: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func preflightDockerCompose(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", "compose", "version")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Skipf("start.sh launcher preflight timed out checking docker compose: %v", ctx.Err())
	}
	if err != nil {
		t.Skipf("start.sh launcher preflight cannot run docker compose: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func preflightLauncherPort(t *testing.T, port int) {
	t.Helper()
	listener, err := net.Listen("tcp4", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Skipf("start.sh launcher preflight requires local port %d to be available: %v", port, err)
	}
	if err := listener.Close(); err != nil {
		t.Skipf("start.sh launcher preflight could not release local port %d: %v", port, err)
	}
}

func startShLauncher(t *testing.T, mode, configPath string, timeout time.Duration) *startShLauncherRun {
	t.Helper()

	repoRoot := launcherRepoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	output := &lockedLauncherOutput{}
	command := exec.CommandContext(ctx, "./start.sh", mode)
	command.Dir = repoRoot
	command.Stdout = output
	command.Stderr = output
	configureTestChildProcess(command)
	command.WaitDelay = 10 * time.Second
	command.Cancel = func() error {
		return signalLauncherProcess(command, syscall.SIGTERM)
	}

	run := &startShLauncherRun{
		ctx:            ctx,
		cancel:         cancel,
		command:        command,
		output:         output,
		done:           make(chan struct{}),
		repoRoot:       repoRoot,
		composeProject: launcherComposeProjectName(t),
		configPath:     configPath,
	}
	run.launcherLock = acquireLauncherFileLock(t)
	command.Env = append(os.Environ(),
		"PRISM_CONFIG_PATH="+configPath,
		"PRISM_DATABASE_COMPOSE_PROJECT="+run.composeProject,
	)
	if err := command.Start(); err != nil {
		cancel()
		run.launcherLock.release(t)
		t.Fatalf("start ./start.sh %s: %v\n%s", mode, err, output.String())
	}

	go func() {
		err := command.Wait()
		run.waitErrMu.Lock()
		run.waitErr = err
		run.waitErrMu.Unlock()
		close(run.done)
	}()

	t.Cleanup(func() {
		run.cleanup(t)
	})

	return run
}

func (r *startShLauncherRun) cleanup(t *testing.T) {
	t.Helper()
	if r.command.Process != nil {
		if exited, _ := r.exited(); !exited {
			if err := signalLauncherProcess(r.command, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Logf("send SIGTERM to start.sh process: %v", err)
			}
			if !r.waitForExit(5 * time.Second) {
				if err := signalLauncherProcess(r.command, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
					t.Logf("send SIGKILL to start.sh process: %v", err)
				}
				if !r.waitForExit(10 * time.Second) {
					t.Logf("start.sh process did not exit after SIGKILL")
				}
			}
		}
	}
	r.cancel()
	r.composeDown(t)
	if r.launcherLock != nil {
		r.launcherLock.release(t)
	}
}

func (r *startShLauncherRun) composeDown(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "docker", "compose", "--project-name", r.composeProject, "down", "--remove-orphans", "--volumes")
	command.Dir = r.repoRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Logf("docker compose cleanup for project %s failed: %v\n%s", r.composeProject, err, strings.TrimSpace(string(output)))
	}
}

func acquireLauncherFileLock(t *testing.T) *launcherFileLock {
	t.Helper()
	path := filepath.Join(os.TempDir(), "prism-start-sh-launcher.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open start.sh launcher lock %s: %v", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatalf("acquire exclusive start.sh launcher lock %s: %v", path, err)
	}
	return &launcherFileLock{file: file, path: path}
}

func (l *launcherFileLock) release(t *testing.T) {
	t.Helper()
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		t.Logf("release start.sh launcher lock %s: %v", l.path, err)
	}
	if err := l.file.Close(); err != nil {
		t.Logf("close start.sh launcher lock %s: %v", l.path, err)
	}
}

func (r *startShLauncherRun) waitForExit(timeout time.Duration) bool {
	select {
	case <-r.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (r *startShLauncherRun) exited() (bool, error) {
	select {
	case <-r.done:
		r.waitErrMu.Lock()
		defer r.waitErrMu.Unlock()
		return true, r.waitErr
	default:
		return false, nil
	}
}

func signalLauncherProcess(command *exec.Cmd, signal syscall.Signal) error {
	return signalTestChildProcess(command, signal)
}

func waitForStartShReadiness(t *testing.T, run *startShLauncherRun, wantFrontend bool) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var backendHealthDetail string
	var frontendHealthDetail string
	for {
		if exited, err := run.exited(); exited {
			t.Fatalf("./start.sh exited before readiness: %v\n%s", err, run.output.String())
		}

		output := run.output.String()
		backendOutputReady := strings.Contains(output, fmt.Sprintf("Backend:  http://localhost:%d\n", launcherBackendPort))
		configOutputReady := strings.Contains(output, "Config:   "+run.configPath+"\n")
		configFileReady := launcherFileExists(run.configPath)
		backendHealthReady, detail := launcherHealthOK(client, fmt.Sprintf("http://127.0.0.1:%d/health", launcherBackendPort))
		backendHealthDetail = detail

		frontendReady := true
		if wantFrontend {
			frontendOutputReady := strings.Contains(output, fmt.Sprintf("Frontend: http://localhost:%d\n", launcherFrontendPort))
			frontendHealthReady, detail := launcherHealthOK(client, fmt.Sprintf("http://127.0.0.1:%d/health", launcherFrontendPort))
			frontendHealthDetail = detail
			frontendReady = frontendOutputReady && frontendHealthReady
		}

		if backendOutputReady && configOutputReady && configFileReady && backendHealthReady && frontendReady {
			return
		}

		select {
		case <-run.ctx.Done():
			t.Fatalf(
				"./start.sh did not become ready: %v\nbackend health: %s\nfrontend health: %s\noutput:\n%s",
				run.ctx.Err(),
				backendHealthDetail,
				frontendHealthDetail,
				output,
			)
		case <-ticker.C:
		}
	}
}

func launcherHealthOK(client *http.Client, url string) (bool, string) {
	response, err := client.Get(url)
	if err != nil {
		return false, err.Error()
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return false, response.Status
	}

	var payload struct {
		Status    string `json:"status"`
		Liveness  string `json:"liveness"`
		Readiness string `json:"readiness"`
		Startup   string `json:"startup"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return false, err.Error()
	}
	if payload.Status != "ok" || payload.Liveness != "ok" || payload.Readiness != "ready" || payload.Startup != "complete" {
		return false, fmt.Sprintf("unexpected health payload %+v", payload)
	}
	return true, "ok"
}

func launcherFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func assertLauncherOutputLine(t *testing.T, run *startShLauncherRun, line string) {
	t.Helper()
	output := run.output.String()
	if !strings.Contains(output, line+"\n") {
		t.Fatalf("expected launcher output line %q, got:\n%s", line, output)
	}
}

func assertLauncherBootstrapConfig(t *testing.T, configPath string) {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read launcher bootstrap config %q: %v", configPath, err)
	}
	var payload struct {
		Server struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"server"`
		Database struct {
			URL string `json:"url"`
		} `json:"database"`
		Runtime struct {
			SideEffects struct {
				AttemptTimeout string `json:"attemptTimeout"`
			} `json:"sideEffects"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode launcher bootstrap config %q: %v", configPath, err)
	}
	if payload.Server.Host != "0.0.0.0" || payload.Server.Port != launcherBackendPort {
		t.Fatalf("expected launcher bootstrap server 0.0.0.0:%d, got %+v", launcherBackendPort, payload.Server)
	}
	if payload.Database.URL != launcherDatabaseURL {
		t.Fatalf("expected launcher bootstrap database URL %q, got %q", launcherDatabaseURL, payload.Database.URL)
	}
	if strings.Contains(string(raw), `"transport"`) {
		t.Fatal("expected launcher bootstrap to omit the removed runtime.transport section")
	}
	if payload.Runtime.SideEffects.AttemptTimeout == "" {
		t.Fatal("expected launcher bootstrap to include sideEffects.attemptTimeout")
	}
}

func launcherRepoRoot(t *testing.T) string {
	t.Helper()
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve integration package directory: %v", err)
	}
	return filepath.Clean(filepath.Join(packageDir, "..", "..", ".."))
}

func launcherComposeProjectName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s-launcher-%d-%s", containername.Prefix(), os.Getpid(), sanitizeLauncherProjectPart(t.Name()))
}

func sanitizeLauncherProjectPart(name string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, character := range strings.ToLower(name) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			builder.WriteRune(character)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	part := strings.Trim(builder.String(), "_")
	if part == "" {
		return "test"
	}
	return part
}
