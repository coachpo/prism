package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type traceContextKey struct{}

type contextObservation struct {
	trace       any
	hasDeadline bool
}

type lifecycleRecorder struct {
	mu       sync.Mutex
	events   []string
	contexts map[string]contextObservation
	calls    map[string]int
}

func newLifecycleRecorder() *lifecycleRecorder {
	return &lifecycleRecorder{
		contexts: map[string]contextObservation{},
		calls:    map[string]int{},
	}
}

func (recorder *lifecycleRecorder) hook(name string) ShutdownHook {
	return func(ctx context.Context) error {
		recorder.record(name, ctx)
		return nil
	}
}

func (recorder *lifecycleRecorder) errorHook(name string, err error) ShutdownHook {
	return func(ctx context.Context) error {
		recorder.record(name, ctx)
		return err
	}
}

func (recorder *lifecycleRecorder) record(name string, ctx context.Context) {
	_, hasDeadline := ctx.Deadline()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, name)
	recorder.calls[name]++
	recorder.contexts[name] = contextObservation{
		trace:       ctx.Value(traceContextKey{}),
		hasDeadline: hasDeadline,
	}
}

func (recorder *lifecycleRecorder) snapshot() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]string(nil), recorder.events...)
}

func (recorder *lifecycleRecorder) callCount(name string) int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.calls[name]
}

func (recorder *lifecycleRecorder) contextFor(name string) (contextObservation, bool) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	observation, exists := recorder.contexts[name]
	return observation, exists
}

type testHTTPServer struct {
	recorder            *lifecycleRecorder
	url                 string
	started             chan struct{}
	shutdownStarted     chan struct{}
	listenAndServe      func() error
	shutdown            func(context.Context) error
	close               func() error
	startOnce           sync.Once
	shutdownStartedOnce sync.Once
}

func newTestHTTPServer(
	recorder *lifecycleRecorder,
	listenAndServe func() error,
	shutdown func(context.Context) error,
	close func() error,
) *testHTTPServer {
	return &testHTTPServer{
		recorder:        recorder,
		started:         make(chan struct{}),
		shutdownStarted: make(chan struct{}),
		listenAndServe:  listenAndServe,
		shutdown:        shutdown,
		close:           close,
	}
}

func (server *testHTTPServer) ListenAndServe() error {
	server.startOnce.Do(func() { close(server.started) })
	if server.listenAndServe == nil {
		return nil
	}
	return server.listenAndServe()
}

func (server *testHTTPServer) Shutdown(ctx context.Context) error {
	if server.recorder != nil {
		server.recorder.record("http shutdown", ctx)
	}
	server.shutdownStartedOnce.Do(func() { close(server.shutdownStarted) })
	if server.shutdown == nil {
		return nil
	}
	return server.shutdown(ctx)
}

func (server *testHTTPServer) Close() error {
	if server.close == nil {
		return nil
	}
	return server.close()
}

func (server *testHTTPServer) URL() string {
	return server.url
}

func (server *testHTTPServer) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
}

func TestAppRunCancellationTriggersLifecycleShutdownOrder(t *testing.T) {
	recorder := newLifecycleRecorder()
	serveDone := make(chan struct{})
	var shutdownOnce sync.Once
	server := newTestHTTPServer(recorder, func() error {
		<-serveDone
		return http.ErrServerClosed
	},
		func(context.Context) error {
			shutdownOnce.Do(func() { close(serveDone) })
			return nil
		},
		nil,
	)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{recorder.hook("side effect drain")},
		SchedulerStop:   recorder.hook("scheduler stop"),
		ServiceClose: []ShutdownHook{
			recorder.hook("runtime service close"),
			recorder.hook("management service close"),
		},
		DBClose: recorder.hook("db close"),
	})
	ctx := context.WithValue(context.Background(), traceContextKey{}, "run-cancel")
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Minute))
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(ctx)
	}()
	server.waitStarted(t)
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error after cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	wantOrder := []string{
		"http shutdown",
		"side effect drain",
		"scheduler stop",
		"runtime service close",
		"management service close",
		"db close",
	}
	assertOrder(t, recorder.snapshot(), wantOrder)
	assertContexts(t, recorder, wantOrder, "run-cancel")
	assertCallCounts(t, recorder, wantOrder, 1)

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown returned error: %v", err)
	}
	assertOrder(t, recorder.snapshot(), wantOrder)
	assertCallCounts(t, recorder, wantOrder, 1)
}

func TestAppShutdownLogsPhaseOrder(t *testing.T) {
	logs := captureLifecycleSlog(t)
	server := newTestHTTPServer(nil, nil, nil, nil)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{func(context.Context) error { return nil }},
		SchedulerStop:   func(context.Context) error { return nil },
		ServiceClose:    []ShutdownHook{func(context.Context) error { return nil }},
		DBClose:         func(context.Context) error { return nil },
	})

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	assertShutdownPhaseLogOrder(t, logs.String(), []string{
		"http_shutdown",
		"side_effect_drain",
		"scheduler_stop",
		"service_close",
		"db_close",
	})
	if !strings.Contains(logs.String(), `"msg":"lifecycle shutdown completed"`) {
		t.Fatalf("expected lifecycle shutdown completed log, got:\n%s", logs.String())
	}
}

func TestAppRunNoDeadlineCancellationUsesDefaultShutdownDeadline(t *testing.T) {
	previousTimeout := defaultShutdownTimeout
	defaultShutdownTimeout = 30 * time.Millisecond
	t.Cleanup(func() { defaultShutdownTimeout = previousTimeout })

	recorder := newLifecycleRecorder()
	serveDone := make(chan struct{})
	var closeOnce sync.Once
	server := newTestHTTPServer(recorder, func() error {
		<-serveDone
		return http.ErrServerClosed
	},
		func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
		func() error {
			closeOnce.Do(func() { close(serveDone) })
			return nil
		},
	)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{recorder.hook("side effect drain")},
		SchedulerStop:   recorder.hook("scheduler stop"),
		DBClose:         recorder.hook("db close"),
	})
	ctx := context.WithValue(context.Background(), traceContextKey{}, "signal-cancel")
	ctx, cancel := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx) }()
	server.waitStarted(t)
	cancel()

	select {
	case err := <-runErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run error %v does not include default shutdown deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run hung after no-deadline cancellation")
	}

	wantOrder := []string{"http shutdown", "side effect drain", "scheduler stop", "db close"}
	assertOrder(t, recorder.snapshot(), wantOrder)
	assertContexts(t, recorder, wantOrder, "signal-cancel")
}

func TestShutdownContextPreservesEarlierCallerDeadline(t *testing.T) {
	previousTimeout := defaultShutdownTimeout
	defaultShutdownTimeout = time.Hour
	t.Cleanup(func() { defaultShutdownTimeout = previousTimeout })

	parentDeadline := time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()
	shutdownCtx, shutdownCancel := shutdownContext(ctx)
	defer shutdownCancel()
	gotDeadline, ok := shutdownCtx.Deadline()
	if !ok {
		t.Fatal("shutdown context missing caller deadline")
	}
	if gotDeadline.Sub(parentDeadline) > time.Millisecond || parentDeadline.Sub(gotDeadline) > time.Millisecond {
		t.Fatalf("shutdown deadline = %v, want caller deadline %v", gotDeadline, parentDeadline)
	}
}

func TestAppShutdownKeepsDBCloseLastAfterHookErrors(t *testing.T) {
	schedulerErr := errors.New("scheduler stop failed")
	serviceErr := errors.New("service close failed")
	recorder := newLifecycleRecorder()
	server := newTestHTTPServer(recorder, nil, nil, nil)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{recorder.hook("side effect drain")},
		SchedulerStop:   recorder.errorHook("scheduler stop", schedulerErr),
		ServiceClose: []ShutdownHook{
			recorder.errorHook("runtime service close", serviceErr),
			recorder.hook("management service close"),
		},
		DBClose: recorder.hook("db close"),
	})
	ctx := context.WithValue(context.Background(), traceContextKey{}, "shutdown")
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Minute))
	defer cancel()

	err := app.Shutdown(ctx)
	for _, wantErr := range []error{schedulerErr, serviceErr} {
		if !errors.Is(err, wantErr) {
			t.Fatalf("Shutdown error %v does not include %v", err, wantErr)
		}
	}

	wantOrder := []string{
		"http shutdown",
		"side effect drain",
		"scheduler stop",
		"runtime service close",
		"management service close",
		"db close",
	}
	assertOrder(t, recorder.snapshot(), wantOrder)
	assertContexts(t, recorder, wantOrder, "shutdown")
	assertCallCounts(t, recorder, wantOrder, 1)

	secondErr := app.Shutdown(ctx)
	for _, wantErr := range []error{schedulerErr, serviceErr} {
		if !errors.Is(secondErr, wantErr) {
			t.Fatalf("second Shutdown error %v does not include %v", secondErr, wantErr)
		}
	}
	assertOrder(t, recorder.snapshot(), wantOrder)
	assertCallCounts(t, recorder, wantOrder, 1)
}

func TestAppRunDrainsInflightHTTPAndRefusesNewRequestsBeforeCleanup(t *testing.T) {
	recorder := newLifecycleRecorder()
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("completed"))
	})
	server := newListenerHTTPServer(t, recorder, handler)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{recorder.hook("side effect drain")},
		SchedulerStop:   recorder.hook("scheduler stop"),
		DBClose:         recorder.hook("db close"),
	})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx) }()

	responseErr := make(chan error, 1)
	go func() {
		response, err := http.Get(server.URL())
		if err != nil {
			responseErr <- err
			return
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			responseErr <- err
			return
		}
		if response.StatusCode != http.StatusOK || string(body) != "completed" {
			responseErr <- errors.New("unexpected in-flight response")
			return
		}
		responseErr <- nil
	}()
	waitForChannel(t, started, "in-flight request start")
	cancel()
	waitForChannel(t, server.shutdownStarted, "HTTP shutdown start")
	assertNewHTTPRequestsFail(t, server.URL())
	close(release)
	if err := <-responseErr; err != nil {
		t.Fatalf("in-flight request did not complete cleanly: %v", err)
	}
	if err := <-runErr; err != nil {
		t.Fatalf("Run returned error after drained shutdown: %v", err)
	}
	assertOrder(t, recorder.snapshot(), []string{"http shutdown", "side effect drain", "scheduler stop", "db close"})
}

func TestAppRunSurfacesHTTPShutdownDeadlineAndStillClosesResources(t *testing.T) {
	recorder := newLifecycleRecorder()
	started := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	})
	server := newListenerHTTPServer(t, recorder, handler)
	app := NewApp(Options{
		HTTPServer:      server,
		SideEffectDrain: []ShutdownHook{recorder.hook("side effect drain")},
		SchedulerStop:   recorder.hook("scheduler stop"),
		DBClose:         recorder.hook("db close"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx) }()
	go func() { _, _ = http.Get(server.URL()) }()
	waitForChannel(t, started, "blocking request start")
	err := <-runErr
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error %v does not include shutdown deadline", err)
	}
	assertOrder(t, recorder.snapshot(), []string{"http shutdown", "side effect drain", "scheduler stop", "db close"})
}

func TestAppRunDistinguishesExpectedServerClosedFromUnexpectedServeError(t *testing.T) {
	serveErr := errors.New("listen failed")
	tests := []struct {
		name       string
		serveErr   error
		wantErr    error
		runContext func() (context.Context, context.CancelFunc)
		wantOrder  []string
		wantTrace  string
		wantCalls  int
	}{
		{
			name:     "expected server closed",
			serveErr: http.ErrServerClosed,
			runContext: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
		},
		{
			name:     "unexpected serve error",
			serveErr: serveErr,
			wantErr:  serveErr,
			runContext: func() (context.Context, context.CancelFunc) {
				ctx := context.WithValue(context.Background(), traceContextKey{}, "serve-error")
				return context.WithDeadline(ctx, time.Now().Add(time.Minute))
			},
			wantOrder: []string{"http shutdown", "db close"},
			wantTrace: "serve-error",
			wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := newLifecycleRecorder()
			server := newTestHTTPServer(recorder, func() error {
				return test.serveErr
			},
				nil,
				nil,
			)
			app := NewApp(Options{
				HTTPServer: server,
				DBClose:    recorder.hook("db close"),
			})
			ctx, cancel := test.runContext()
			defer cancel()

			err := app.Run(ctx)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Run error %v does not include %v", err, test.wantErr)
			}
			assertOrder(t, recorder.snapshot(), test.wantOrder)
			if len(test.wantOrder) == 0 {
				return
			}
			assertContexts(t, recorder, test.wantOrder, test.wantTrace)
			assertCallCounts(t, recorder, test.wantOrder, test.wantCalls)
		})
	}
}

func newListenerHTTPServer(t *testing.T, recorder *lifecycleRecorder, handler http.Handler) *testHTTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	httpServer := &http.Server{Handler: handler}
	server := newTestHTTPServer(recorder, func() error {
		return httpServer.Serve(listener)
	},
		func(ctx context.Context) error {
			return httpServer.Shutdown(ctx)
		},
		func() error {
			return httpServer.Close()
		},
	)
	server.url = "http://" + listener.Addr().String()
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func waitForChannel(t *testing.T, channel <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func assertNewHTTPRequestsFail(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err != nil {
			return
		}
		_ = response.Body.Close()
		lastErr = errors.New("request unexpectedly succeeded after shutdown started")
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("new request did not fail after shutdown started: %v", lastErr)
}

func assertOrder(t *testing.T, got []string, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown order mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func assertContexts(t *testing.T, recorder *lifecycleRecorder, names []string, wantTrace string) {
	t.Helper()
	for _, name := range names {
		observation, exists := recorder.contextFor(name)
		if !exists {
			t.Fatalf("missing context for %s", name)
		}
		if observation.trace != wantTrace {
			t.Fatalf("%s saw trace %v, want %s", name, observation.trace, wantTrace)
		}
		if !observation.hasDeadline {
			t.Fatalf("%s did not receive shutdown deadline", name)
		}
	}
}

func assertCallCounts(t *testing.T, recorder *lifecycleRecorder, names []string, want int) {
	t.Helper()
	for _, name := range names {
		if got := recorder.callCount(name); got != want {
			t.Fatalf("%s called %d times, want %d", name, got, want)
		}
	}
}

func captureLifecycleSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	return &output
}

func assertShutdownPhaseLogOrder(t *testing.T, logText string, want []string) {
	t.Helper()
	var got []string
	for line := range strings.SplitSeq(strings.TrimSpace(logText), "\n") {
		if !strings.Contains(line, `"msg":"lifecycle shutdown phase started"`) {
			continue
		}
		for _, phase := range want {
			if strings.Contains(line, `"phase":"`+phase+`"`) {
				got = append(got, phase)
				break
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shutdown phase logs mismatch\ngot:  %v\nwant: %v\nlogs:\n%s", got, want, logText)
	}
}
