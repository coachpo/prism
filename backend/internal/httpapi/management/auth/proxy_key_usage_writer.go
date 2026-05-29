package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	defaultProxyKeyUsageFlushInterval   = 500 * time.Millisecond
	defaultProxyKeyUsageShutdownTimeout = 3 * time.Second
	defaultProxyKeyUsageWriteTimeout    = 5 * time.Second
)

type proxyAPIKeyUsageUpdate struct {
	KeyID      int
	LastUsedAt time.Time
	LastUsedIP string
}

type proxyAPIKeyUsageWriter struct {
	write func(context.Context, int, time.Time, string) error

	flushInterval   time.Duration
	shutdownTimeout time.Duration
	writeTimeout    time.Duration
	scheduler       *background.Scheduler
	ownsScheduler   bool

	mu      sync.Mutex
	pending map[int]proxyAPIKeyUsageUpdate
	closed  bool
}

func newProxyAPIKeyUsageWriter(write func(context.Context, int, time.Time, string) error, scheduler *background.Scheduler) *proxyAPIKeyUsageWriter {
	if write == nil {
		return nil
	}
	writer := &proxyAPIKeyUsageWriter{
		write:           write,
		flushInterval:   defaultProxyKeyUsageFlushInterval,
		shutdownTimeout: defaultProxyKeyUsageShutdownTimeout,
		writeTimeout:    defaultProxyKeyUsageWriteTimeout,
		scheduler:       scheduler,
		pending:         map[int]proxyAPIKeyUsageUpdate{},
	}
	if writer.scheduler == nil {
		writer.scheduler = background.NewScheduler(background.Config{})
		writer.ownsScheduler = true
		_ = writer.RegisterBackgroundWorker(writer.scheduler)
		_ = writer.scheduler.Start(context.Background())
	}
	return writer
}

func (w *proxyAPIKeyUsageWriter) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if w == nil || scheduler == nil {
		return nil
	}
	w.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("proxy_key_usage_writer"),
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       256,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainFlush,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: w.flushInterval},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: w.flushInterval},
		Timeout:          w.writeTimeout,
	}, w.handleScheduledFlush)
}

func (w *proxyAPIKeyUsageWriter) Enqueue(keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	if w == nil || keyID <= 0 {
		asyncmetrics.RecordOutcome(context.Background(), "proxy_key_usage_writer", "enqueue", asyncmetrics.OutcomeInvalid)
		return nil
	}
	update := proxyAPIKeyUsageUpdate{KeyID: keyID, LastUsedAt: lastUsedAt.UTC(), LastUsedIP: lastUsedIP}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		asyncmetrics.RecordOutcome(context.Background(), "proxy_key_usage_writer", "enqueue", asyncmetrics.OutcomeUnavailable)
		return fmt.Errorf("proxy api key usage writer closed")
	}
	if existing, ok := w.pending[keyID]; ok {
		if existing.LastUsedAt.After(update.LastUsedAt) {
			asyncmetrics.RecordQueueDepth(context.Background(), "proxy_key_usage_writer", "pending", int64(len(w.pending)))
			asyncmetrics.RecordOutcome(context.Background(), "proxy_key_usage_writer", "enqueue", asyncmetrics.OutcomeSkipped)
			return nil
		}
		if existing.LastUsedAt.Equal(update.LastUsedAt) && strings.TrimSpace(update.LastUsedIP) == "" {
			update.LastUsedIP = existing.LastUsedIP
		}
	}
	w.pending[keyID] = update
	asyncmetrics.RecordQueueDepth(context.Background(), "proxy_key_usage_writer", "pending", int64(len(w.pending)))
	if w.scheduler != nil {
		_ = w.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("proxy_key_usage_writer"), CoalesceKey: "proxy_key_usage_writer"})
	}
	asyncmetrics.RecordOutcome(context.Background(), "proxy_key_usage_writer", "enqueue", asyncmetrics.OutcomeAccepted)
	return nil
}

func (w *proxyAPIKeyUsageWriter) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	if w.ownsScheduler && w.scheduler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), w.shutdownTimeout)
		_ = w.scheduler.Stop(ctx, time.Now().Add(w.shutdownTimeout))
		cancel()
	} else {
		w.flushPending(w.shutdownTimeout)
	}
}

func (w *proxyAPIKeyUsageWriter) handleScheduledFlush(ctx context.Context, _ background.Job) background.JobResult {
	if err := w.flushPendingContext(ctx); err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (w *proxyAPIKeyUsageWriter) flushPending(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := w.flushPendingContext(ctx); err != nil {
		slog.Error("failed to flush proxy api key usage updates", "error", err)
	}
}

func (w *proxyAPIKeyUsageWriter) flushPendingContext(ctx context.Context) error {
	startedAt := time.Now()
	updates := w.drainPending()
	asyncmetrics.RecordBatchSize(ctx, "proxy_key_usage_writer", "flush", int64(len(updates)))
	if len(updates) == 0 {
		asyncmetrics.RecordDuration(ctx, "proxy_key_usage_writer", "flush", asyncmetrics.OutcomeSkipped, time.Since(startedAt))
		return nil
	}
	asyncmetrics.AddInflight(ctx, "proxy_key_usage_writer", "flush", 1)
	defer asyncmetrics.AddInflight(ctx, "proxy_key_usage_writer", "flush", -1)
	for _, update := range updates {
		if err := w.write(ctx, update.KeyID, update.LastUsedAt, update.LastUsedIP); err != nil {
			w.requeue(updates)
			asyncmetrics.RecordRetry(ctx, "proxy_key_usage_writer", "flush", asyncmetrics.OutcomeRetryScheduled)
			asyncmetrics.RecordDuration(ctx, "proxy_key_usage_writer", "flush", asyncmetrics.OutcomeFromError(err), time.Since(startedAt))
			slog.Error("failed to flush proxy api key usage updates", "error", err, "pending_keys", len(updates))
			return err
		}
	}
	asyncmetrics.RecordDuration(ctx, "proxy_key_usage_writer", "flush", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	return nil
}

func (w *proxyAPIKeyUsageWriter) drainPending() []proxyAPIKeyUsageUpdate {
	w.mu.Lock()
	defer w.mu.Unlock()
	updates := make([]proxyAPIKeyUsageUpdate, 0, len(w.pending))
	for keyID, update := range w.pending {
		updates = append(updates, update)
		delete(w.pending, keyID)
	}
	asyncmetrics.RecordQueueDepth(context.Background(), "proxy_key_usage_writer", "pending", int64(len(w.pending)))
	return updates
}

func (w *proxyAPIKeyUsageWriter) requeue(updates []proxyAPIKeyUsageUpdate) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, update := range updates {
		existing, ok := w.pending[update.KeyID]
		if ok {
			if existing.LastUsedAt.After(update.LastUsedAt) {
				continue
			}
			if existing.LastUsedAt.Equal(update.LastUsedAt) && strings.TrimSpace(update.LastUsedIP) == "" {
				update.LastUsedIP = existing.LastUsedIP
			}
		}
		w.pending[update.KeyID] = update
	}
	asyncmetrics.RecordQueueDepth(context.Background(), "proxy_key_usage_writer", "pending", int64(len(w.pending)))
}
